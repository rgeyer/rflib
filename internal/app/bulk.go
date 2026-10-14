package app

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/ryan/rflib/internal/domain"
	"github.com/ryan/rflib/internal/query"
	"github.com/ryan/rflib/internal/store"
)

// MinIDPrefix is the shortest ID prefix accepted in place of a full ID.
const MinIDPrefix = 4

// ChannelID resolves a channel reference: a full ID, or a prefix of one at
// least MinIDPrefix long that matches a single channel in the library.
func (s *Service) ChannelID(ctx context.Context, ref string) (string, error) {
	ids, err := s.ChannelIDs(ctx, []string{ref})
	if err != nil {
		return "", err
	}
	return ids[0], nil
}

// ChannelIDs resolves channel references (see ChannelID), in order.
func (s *Service) ChannelIDs(ctx context.Context, refs []string) ([]string, error) {
	var all []string
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		ref = strings.TrimSpace(ref)
		if _, err := s.lib.GetChannel(ctx, ref); err == nil {
			out = append(out, ref)
			continue
		} else if !errors.Is(err, store.ErrNotFound) {
			return nil, err
		}
		if len(ref) < MinIDPrefix {
			return nil, fmt.Errorf("channel %q: %w", ref, store.ErrNotFound)
		}
		if all == nil {
			cs, err := s.lib.ListChannels(ctx)
			if err != nil {
				return nil, err
			}
			for _, c := range cs {
				all = append(all, c.ID)
			}
		}
		var found []string
		for _, id := range all {
			if strings.HasPrefix(id, ref) {
				found = append(found, id)
			}
		}
		switch len(found) {
		case 0:
			return nil, fmt.Errorf("channel %q: %w", ref, store.ErrNotFound)
		case 1:
			out = append(out, found[0])
		default:
			return nil, fmt.Errorf("channel ID %q is ambiguous: it starts %d channels; give more of it", ref, len(found))
		}
	}
	return out, nil
}

// Selection picks channels: by ID (or ID prefix), by query, or both, in
// which case a channel must be in both. A nil Query and no IDs select
// nothing; All selects every channel in scope.
type Selection struct {
	IDs   []string
	Query query.Node
	All   bool
}

// Bulk reports whether the selection can pick more than one channel.
func (sel Selection) Bulk() bool { return sel.All || sel.Query != nil || len(sel.IDs) > 1 }

func (sel Selection) empty() bool { return !sel.All && sel.Query == nil && len(sel.IDs) == 0 }

// Select returns the selected channels in name order.
func (s *Service) Select(ctx context.Context, sel Selection) ([]domain.Channel, error) {
	return s.selectIn(ctx, sel, nil)
}

// selectIn is Select limited to the channels in scope, when scope isn't
// nil.
func (s *Service) selectIn(ctx context.Context, sel Selection, scope map[string]bool) ([]domain.Channel, error) {
	if sel.empty() {
		return nil, fmt.Errorf("select channels by ID, by query, or all of them")
	}
	ids, err := s.ChannelIDs(ctx, sel.IDs)
	if err != nil {
		return nil, err
	}
	var cs []domain.Channel
	if sel.Query != nil {
		if cs, err = s.FindChannels(ctx, sel.Query); err != nil {
			return nil, err
		}
	} else if cs, err = s.lib.ListChannels(ctx); err != nil {
		return nil, err
	}
	return slices.DeleteFunc(cs, func(c domain.Channel) bool {
		return (len(ids) > 0 && !slices.Contains(ids, c.ID)) || (scope != nil && !scope[c.ID])
	}), nil
}

// TagEdit changes a channel's tags: Set replaces them when not nil;
// otherwise Add and Remove change them.
type TagEdit struct {
	Set         []string
	Add, Remove []string
}

// Apply returns tags changed by the edit.
func (e TagEdit) Apply(tags []string) []string {
	if e.Set != nil {
		return domain.NormalizeTags(e.Set)
	}
	remove := domain.NormalizeTags(e.Remove)
	out := slices.DeleteFunc(slices.Clone(tags), func(t string) bool { return slices.Contains(remove, t) })
	return domain.NormalizeTags(append(out, e.Add...))
}

// ChannelEdit is a change to apply to many channels at once. It holds only
// fields that make sense in bulk; frequencies, tones, mode, and names are
// changed one channel at a time. Nil fields are left alone.
type ChannelEdit struct {
	Tags    *TagEdit
	Skip    *bool
	Power   *domain.PowerIntent
	Service *domain.Service
	// RXOnly, when nil, is turned on (never off) by a listen-only Service.
	RXOnly *bool
}

// Apply changes one channel as the edit says.
func (e ChannelEdit) Apply(c *domain.Channel) {
	if e.Tags != nil {
		c.Tags = e.Tags.Apply(c.Tags)
	}
	if e.Skip != nil {
		c.Skip = *e.Skip
	}
	if e.Power != nil {
		c.Power = *e.Power
	}
	if e.Service != nil {
		c.Service = *e.Service
		c.RXOnly = c.RXOnly || domain.DefaultRXOnly(c.Service)
	}
	if e.RXOnly != nil {
		c.RXOnly = *e.RXOnly
	}
}

// UpdateChannels applies an edit to every selected channel in one
// transaction, and returns the channels as saved and their band-plan
// warnings, each prefixed with the channel's name.
func (s *Service) UpdateChannels(ctx context.Context, sel Selection, e ChannelEdit) ([]domain.Channel, []string, error) {
	cs, err := s.Select(ctx, sel)
	if err != nil {
		return nil, nil, err
	}
	var warnings []string
	err = s.lib.InTx(ctx, func(tx store.Library) error {
		for i := range cs {
			e.Apply(&cs[i])
			saved, err := tx.UpdateChannel(ctx, cs[i])
			if err != nil {
				return fmt.Errorf("%s: %w", cs[i].Name, err)
			}
			cs[i] = saved
			for _, w := range domain.CheckBandPlan(saved) {
				warnings = append(warnings, saved.Name+": "+w)
			}
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return cs, warnings, nil
}

// DeleteChannels deletes the selected channels in one transaction, taking
// them off every radio, and returns them.
func (s *Service) DeleteChannels(ctx context.Context, sel Selection) ([]domain.Channel, error) {
	cs, err := s.Select(ctx, sel)
	if err != nil {
		return nil, err
	}
	err = s.lib.InTx(ctx, func(tx store.Library) error {
		for _, c := range cs {
			if err := tx.DeleteChannel(ctx, c.ID); err != nil {
				return fmt.Errorf("%s: %w", c.Name, err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return cs, nil
}
