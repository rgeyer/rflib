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

	"github.com/ryan/rflib/internal/domain"
	"github.com/ryan/rflib/internal/importer"
	"github.com/ryan/rflib/internal/model"
	"github.com/ryan/rflib/internal/query"
	"github.com/ryan/rflib/internal/store"
)

// Service is the operations on one library.
type Service struct {
	lib store.Library
}

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
	return New(s.Library(lib.ID)), nil
}

// LibraryID is the ID of the Service's library.
func (s *Service) LibraryID() string { return s.lib.ID() }

// --- groups ---

func (s *Service) CreateGroup(ctx context.Context, g domain.Group) (domain.Group, error) {
	return s.lib.CreateGroup(ctx, g)
}

func (s *Service) GetGroup(ctx context.Context, idOrName string) (domain.Group, error) {
	return s.lib.GetGroup(ctx, idOrName)
}

func (s *Service) ListGroups(ctx context.Context) ([]domain.Group, error) {
	return s.lib.ListGroups(ctx)
}

func (s *Service) UpdateGroup(ctx context.Context, g domain.Group) (domain.Group, error) {
	return s.lib.UpdateGroup(ctx, g)
}

func (s *Service) DeleteGroup(ctx context.Context, idOrName string) error {
	return s.lib.DeleteGroup(ctx, idOrName)
}

// --- channels ---

// CreateChannel stores a channel and returns its band-plan warnings (see
// domain.CheckBandPlan), which don't stop it being saved.
func (s *Service) CreateChannel(ctx context.Context, c domain.Channel) (domain.Channel, []string, error) {
	c, err := s.lib.CreateChannel(ctx, c)
	if err != nil {
		return c, nil, err
	}
	return c, domain.CheckBandPlan(c), nil
}

// UpdateChannel saves a channel and returns its band-plan warnings.
func (s *Service) UpdateChannel(ctx context.Context, c domain.Channel) (domain.Channel, []string, error) {
	c, err := s.lib.UpdateChannel(ctx, c)
	if err != nil {
		return c, nil, err
	}
	return c, domain.CheckBandPlan(c), nil
}

func (s *Service) GetChannel(ctx context.Context, id string) (domain.Channel, error) {
	return s.lib.GetChannel(ctx, id)
}

func (s *Service) DeleteChannel(ctx context.Context, id string) error {
	return s.lib.DeleteChannel(ctx, id)
}

// ListChannels returns every channel in name order.
func (s *Service) ListChannels(ctx context.Context) ([]domain.Channel, error) {
	return s.lib.ListChannels(ctx)
}

// QueryEnv resolves what queries refer to: saved searches, groups, radios,
// and radio models.
func (s *Service) QueryEnv(ctx context.Context) query.Env {
	return query.Env{
		Search: func(name string) (string, error) {
			x, err := s.lib.GetSearch(ctx, name)
			return x.Query, err
		},
		Group: func(ref string) (string, error) {
			g, err := s.lib.GetGroup(ctx, ref)
			return g.ID, err
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
		Tunes: func(ref string) (func(int64) bool, error) {
			m, err := s.radioOrModel(ctx, ref)
			if err != nil {
				return nil, err
			}
			return m.Tunes, nil
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
	return model.Model{}, fmt.Errorf("no radio or radio model named %q", ref)
}

// FindChannels returns the channels matching a query, in name order.
func (s *Service) FindChannels(ctx context.Context, n query.Node) ([]domain.Channel, error) {
	m, err := query.Compile(n, s.QueryEnv(ctx))
	if err != nil {
		return nil, err
	}
	all, err := s.lib.ListChannels(ctx)
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
func (s *Service) Import(ctx context.Context, b importer.Batch, opts importer.Options) (importer.Summary, error) {
	return importer.Apply(ctx, s.lib, b, opts)
}
