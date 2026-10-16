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
	"slices"
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
	if errors.Is(err, store.ErrNotFound) && library == "" {
		return nil, fmt.Errorf("there is no default library; run rflib init: %w", err)
	}
	if errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("no library named %q: %w", library, err)
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
// domain.CheckBandPlan) and warnings for tags that repeat its fields (see
// addedTagRepeats), which don't stop it being saved.
func (s *Service) CreateChannel(ctx context.Context, c domain.Channel) (domain.Channel, []string, error) {
	if err := s.writable(); err != nil {
		return c, nil, err
	}
	c, err := s.lib.CreateChannel(ctx, c)
	if err != nil {
		return c, nil, err
	}
	return c, append(domain.CheckBandPlan(c), addedTagRepeats(domain.Channel{}, c)...), nil
}

// addedTagRepeats warns about the tags after has and before didn't that
// repeat one of the channel's fields (see query.TagRepeats). Only added
// tags warn, so a tag kept from before doesn't warn on every save.
func addedTagRepeats(before, after domain.Channel) []string {
	added := after
	added.Tags = slices.DeleteFunc(slices.Clone(after.Tags), func(t string) bool { return slices.Contains(before.Tags, t) })
	return query.TagRepeats(added)
}

// UpdateChannel saves a channel and returns its band-plan and added-tag
// warnings. A link can change only what's its own (see LinkedEdit).
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
	return c, append(domain.CheckBandPlan(c), addedTagRepeats(old, c)...), nil
}

// GetChannel looks a channel up by ID or unique ID prefix (see ChannelID).
func (s *Service) GetChannel(ctx context.Context, ref string) (domain.Channel, error) {
	id, err := s.ChannelID(ctx, ref)
	if err != nil {
		return domain.Channel{}, err
	}
	return s.lib.GetChannel(ctx, id)
}

// ListChannels returns every channel in name order (see
// domain.CompareChannels).
func (s *Service) ListChannels(ctx context.Context) ([]domain.Channel, error) {
	return s.lib.ListChannels(ctx)
}

// QueryEnv resolves what queries refer to: saved searches, radios, radio
// models, and systems.
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
		System: func(ref string) (map[string]bool, error) { return s.systemChannels(ctx, ref) },
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

// FindChannels returns the channels matching a query, in name order,
// among those its in: scope covers: by default the channels in use (see
// scoped).
func (s *Service) FindChannels(ctx context.Context, n query.Node) ([]domain.Channel, error) {
	scope, n, err := query.SplitScope(n)
	if err != nil {
		return nil, err
	}
	m, err := query.Compile(n, s.QueryEnv(ctx))
	if err != nil {
		return nil, err
	}
	all, err := s.scoped(ctx, scope, false)
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

// ListedChannels is FindChannels for the main channel list, which leaves
// out retired channels unless the search says status: itself (directly or
// in a saved search it uses). It returns how many retired channels it
// left out. Other searches, such as a radio's sources, never do this.
func (s *Service) ListedChannels(ctx context.Context, n query.Node) ([]domain.Channel, int, error) {
	cs, err := s.FindChannels(ctx, n)
	if err != nil {
		return nil, 0, err
	}
	if s.mentionsStatus(ctx, n, map[string]bool{}) {
		return cs, 0, nil
	}
	hidden := 0
	cs = slices.DeleteFunc(cs, func(c domain.Channel) bool {
		retired := c.Status.OrActive() == domain.StatusRetired
		if retired {
			hidden++
		}
		return retired
	})
	return cs, hidden, nil
}

// mentionsStatus reports whether a query, or a saved search it uses, has
// a status: term.
func (s *Service) mentionsStatus(ctx context.Context, n query.Node, seen map[string]bool) bool {
	if query.Mentions(n, "status") {
		return true
	}
	for _, name := range query.Refs(n) {
		if seen[name] {
			continue
		}
		seen[name] = true
		ss, err := s.lib.GetSearch(ctx, name)
		if err != nil {
			continue
		}
		if sn, err := query.Parse(ss.Query); err == nil && s.mentionsStatus(ctx, sn, seen) {
			return true
		}
	}
	return false
}

// MoreInShared counts the channels a search of what's in use (or of your
// own channels) leaves out that the whole shared list matches: shared
// channels you haven't used yet, less those you've copied. found is how
// many the search found. It's 0 for a search of the shared list, and in the
// shared library itself.
func (s *Service) MoreInShared(ctx context.Context, n query.Node, found int) (int, error) {
	scope, rest, err := query.SplitScope(n)
	if err != nil || s.info.Shared || (scope != "" && scope != "use" && scope != "mine") {
		return 0, err
	}
	if rest == nil {
		rest = query.Join()
	}
	m, err := query.Compile(rest, s.QueryEnv(ctx))
	if err != nil {
		return 0, err
	}
	all, err := s.scoped(ctx, "all", true)
	if err != nil {
		return 0, err
	}
	matched := 0
	for _, c := range all {
		if m(c) {
			matched++
		}
	}
	return max(matched-found, 0), nil
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
		// Organizations belong to shared channels.
		c.Status, c.StatusNote, c.BackOn, c.ReplacedBy, c.Orgs = domain.StatusActive, "", nil, "", nil
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
