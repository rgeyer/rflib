// Package app is rflib's service layer: the operations the CLI and the web
// API offer, on one library. It owns the rules that span records, such as
// what a saved search may use, which channels a radio can take, and how a
// sync places them, so every front end applies them the same way. Front
// ends parse input and present results; they don't touch the store.
package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ryan/rflib/internal/domain"
	"github.com/ryan/rflib/internal/importer"
	"github.com/ryan/rflib/internal/model"
	"github.com/ryan/rflib/internal/query"
	"github.com/ryan/rflib/internal/store"
)

// Service is the operations on one library.
type Service struct {
	lib store.Library
	// info is the library's record, and st the store it's in, which
	// shared operations reach the shared library through. A Service from
	// New has neither.
	info domain.Library
	st   store.Store
}

// New returns the operations on lib, without access to the shared library.
func New(lib store.Library) *Service { return &Service{lib: lib} }

// Open resolves a library by ID or name and returns its Service.
func Open(ctx context.Context, s store.Store, library string) (*Service, error) {
	lib, err := s.GetLibrary(ctx, library)
	if errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("no library named %q; see rflib library list: %w", library, err)
	}
	if err != nil {
		return nil, err
	}
	return &Service{lib: s.Library(lib.ID), info: lib, st: s}, nil
}

// ErrReadOnly is the shared library refusing an everyday change (HTTP
// 403): it changes only through the shared operations.
var ErrReadOnly = errors.New("the shared library changes only through rflib shared commands")

// writable refuses changes to the shared library outside the shared
// operations.
func (s *Service) writable() error {
	if s.info.Shared {
		return ErrReadOnly
	}
	return nil
}

// LibraryID is the ID of the Service's library.
func (s *Service) LibraryID() string { return s.lib.ID() }

// Library is the Service's library record (empty from New).
func (s *Service) Library() domain.Library { return s.info }

// ChangedSince returns the library's channels changed after since, and a
// cursor for next time; see store.Library.ChangedSince.
func (s *Service) ChangedSince(ctx context.Context, since time.Time) ([]domain.Channel, time.Time, error) {
	return s.lib.ChangedSince(ctx, since)
}

// --- channels ---

// CreateChannel stores a channel and returns its band-plan warnings (see
// domain.CheckBandPlan), which don't stop it being saved.
func (s *Service) CreateChannel(ctx context.Context, c domain.Channel) (domain.Channel, []string, error) {
	if err := s.writable(); err != nil {
		return c, nil, err
	}
	c, err := s.lib.CreateChannel(ctx, c)
	if err != nil {
		return c, nil, err
	}
	return c, domain.CheckBandPlan(c), nil
}

// UpdateChannel saves a channel and returns its band-plan warnings. A link
// can change only what's its own (see LinkedEdit).
func (s *Service) UpdateChannel(ctx context.Context, c domain.Channel) (domain.Channel, []string, error) {
	if err := s.writable(); err != nil {
		return c, nil, err
	}
	old, err := s.lib.GetChannel(ctx, c.ID)
	if err != nil {
		return c, nil, err
	}
	if err := LinkedEdit(old, c); err != nil {
		return c, nil, err
	}
	c, err = s.lib.UpdateChannel(ctx, c)
	if err != nil {
		return c, nil, err
	}
	return c, domain.CheckBandPlan(c), nil
}

// GetChannel looks a channel up by ID or unique ID prefix (see ChannelID).
func (s *Service) GetChannel(ctx context.Context, ref string) (domain.Channel, error) {
	id, err := s.ChannelID(ctx, ref)
	if err != nil {
		return domain.Channel{}, err
	}
	return s.lib.GetChannel(ctx, id)
}

// DeleteChannel deletes a channel by ID or unique ID prefix.
func (s *Service) DeleteChannel(ctx context.Context, ref string) error {
	if err := s.writable(); err != nil {
		return err
	}
	id, err := s.ChannelID(ctx, ref)
	if err != nil {
		return err
	}
	return s.lib.DeleteChannel(ctx, id)
}

// ListChannels returns every channel in name order (see
// domain.CompareChannels).
func (s *Service) ListChannels(ctx context.Context) ([]domain.Channel, error) {
	return s.lib.ListChannels(ctx)
}

// QueryEnv resolves what queries refer to: saved searches, radios, and
// radio models.
func (s *Service) QueryEnv(ctx context.Context) query.Env {
	return query.Env{
		Search: func(name string) (string, error) {
			x, err := s.lib.GetSearch(ctx, name)
			return x.Query, err
		},
		RadioChannels: func(ref string) (map[string]bool, error) {
			if ref == "" {
				return s.lib.RadioChannelIDs(ctx, "")
			}
			r, err := s.lib.GetRadio(ctx, ref)
			if err != nil {
				return nil, err
			}
			return s.lib.RadioChannelIDs(ctx, r.ID)
		},
		Tunes: func(ref string) (func(domain.Channel) bool, error) {
			m, err := s.radioOrModel(ctx, ref)
			if err != nil {
				return nil, err
			}
			return m.Receives, nil
		},
	}
}

// radioOrModel returns a radio's model, or the model named ref.
func (s *Service) radioOrModel(ctx context.Context, ref string) (model.Model, error) {
	r, err := s.lib.GetRadio(ctx, ref)
	if err == nil {
		return model.Find(r.Model)
	}
	if !errors.Is(err, store.ErrNotFound) {
		return model.Model{}, err
	}
	if m, err := model.Find(ref); err == nil {
		return m, nil
	}
	return model.Model{}, domain.Invalidf("no radio or radio model named %q", ref)
}

// FindChannels returns the channels matching a query, in name order. With
// in:shared it searches the whole shared list instead of the library.
func (s *Service) FindChannels(ctx context.Context, n query.Node) ([]domain.Channel, error) {
	scope, n, err := query.SplitScope(n)
	if err != nil {
		return nil, err
	}
	m, err := query.Compile(n, s.QueryEnv(ctx))
	if err != nil {
		return nil, err
	}
	from := s.lib
	if scope == "shared" {
		if from, err = s.sharedLibrary(ctx); err != nil {
			return nil, err
		}
	}
	all, err := from.ListChannels(ctx)
	if err != nil {
		return nil, err
	}
	out := all[:0]
	for _, c := range all {
		if m(c) {
			out = append(out, c)
		}
	}
	return out, nil
}

// Find parses query text and returns the matching channels.
func (s *Service) Find(ctx context.Context, text string) ([]domain.Channel, error) {
	n, err := query.Parse(text)
	if err != nil {
		return nil, err
	}
	return s.FindChannels(ctx, n)
}

// --- imports ---

// Import writes a parsed batch in one transaction; see importer.Apply.
// Imported channels are private and active. A link in a backup stays a
// link when its shared channel is in this database's shared list, and
// otherwise becomes a private copy, with a warning.
func (s *Service) Import(ctx context.Context, b importer.Batch, opts importer.Options) (importer.Summary, error) {
	if err := s.writable(); err != nil {
		return importer.Summary{}, err
	}
	var copied []importer.RowError
	for i := range b.Records {
		r := &b.Records[i]
		c := &r.Channel
		if c.Link != nil {
			if _, err := s.sharedChannel(ctx, c.Link.SharedID); err == nil {
				continue
			} else if !errors.Is(err, store.ErrNotFound) {
				return importer.Summary{}, err
			}
			c.Tags, c.Notes, c.CopiedFrom, c.Link = c.AllTags(), c.AllNotes(), c.Link.SharedID, nil
			copied = append(copied, importer.RowError{Row: r.Row, Err: domain.Invalidf("%s: its shared channel isn't in this database's shared list, so it's a private copy", c.Name)})
		}
		c.Status, c.StatusNote, c.BackOn, c.ReplacedBy = domain.StatusActive, "", nil, ""
	}
	sum, err := importer.Apply(ctx, s.lib, b, opts)
	sum.Warnings = append(copied, sum.Warnings...)
	return sum, err
}

// sharedChannel returns a channel of the shared list by ID.
func (s *Service) sharedChannel(ctx context.Context, id string) (domain.Channel, error) {
	sh, err := s.sharedLibrary(ctx)
	if err != nil {
		return domain.Channel{}, err
	}
	return sh.GetChannel(ctx, id)
}
