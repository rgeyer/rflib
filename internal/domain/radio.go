package domain

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"
)

// Radio is one of the user's own radios: a model, the channels programmed
// into it in memory order, and per-radio choices.
type Radio struct {
	ID    string
	Name  string
	Model string
	// DefaultPower applies to channels without a power intent of their own
	// or from their group.
	DefaultPower PowerIntent
	// Dedupe is how duplicate names on this radio are told apart: none,
	// index, or freq (see package naming).
	Dedupe    string
	Notes     string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// RadioSource is one of a radio's searches. `radio sync` appends the
// channels it matches that aren't on the radio yet to the source's part of
// the list, in Sort order.
type RadioSource struct {
	ID      string
	RadioID string
	// Position orders a radio's sources, from 0. Earlier sources claim a
	// channel first.
	Position int
	// Query is the search, in package query's language.
	Query string
	// Sort orders the channels a sync adds: name, freq, or group.
	Sort string
	// SectionStart, when set, starts the source's first channel at that
	// memory number.
	SectionStart *int
}

// SortOrders are the orders channels can be added in.
var SortOrders = []string{"name", "freq", "group"}

func ValidateRadioSource(src RadioSource) error {
	if strings.TrimSpace(src.Query) == "" {
		return fmt.Errorf("a source needs a query")
	}
	if !slices.Contains(SortOrders, src.Sort) {
		return fmt.Errorf("unknown sort %q; use name, freq, or group", src.Sort)
	}
	if src.SectionStart != nil && *src.SectionStart < 0 {
		return fmt.Errorf("section start must not be negative")
	}
	return nil
}

// SavedSearch is a named query, used as @name in other queries and in
// radio sources.
type SavedSearch struct {
	ID        string
	Name      string
	Query     string
	Notes     string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ValidSearchName reports whether name can name a saved search: lower-case
// letters, digits, -, _, and ., starting with a letter or digit.
func ValidSearchName(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		case i > 0 && (r == '-' || r == '_' || r == '.'):
		default:
			return false
		}
	}
	return true
}

func ValidateSavedSearch(s SavedSearch) error {
	if !ValidSearchName(s.Name) {
		return fmt.Errorf("search name %q must be lower-case letters, digits, -, _, and ., starting with a letter or digit", s.Name)
	}
	if strings.TrimSpace(s.Query) == "" {
		return fmt.Errorf("a saved search needs a query")
	}
	return nil
}

// RadioEntry is a channel's place on a radio and the radio's overrides for
// it. Unset overrides fall back to the channel.
type RadioEntry struct {
	Channel Channel
	// Position is the entry's index in the radio's list, from 0.
	Position int
	// SectionStart, when set, starts this entry at that memory number; the
	// entries after it continue from there.
	SectionStart *int
	Name         string
	Power        PowerIntent
	Skip         *bool
	// SourceID is the radio source that added the entry; empty for
	// channels added by hand.
	SourceID string
}

// MemoryNumbers assigns memory numbers to entries in list order: contiguous
// from first, jumping ahead at each section start. A section start that
// would move backwards is an error.
func MemoryNumbers(entries []RadioEntry, first int) ([]int, error) {
	out := make([]int, len(entries))
	next := first
	for i, e := range entries {
		if e.SectionStart != nil {
			if *e.SectionStart < next {
				return nil, fmt.Errorf("%q starts a section at memory %d, but the channels before it already reach memory %d", e.Channel.Name, *e.SectionStart, next-1)
			}
			next = *e.SectionStart
		}
		out[i] = next
		next++
	}
	return out, nil
}

func ValidateRadio(r Radio) error {
	if strings.TrimSpace(r.Name) == "" {
		return fmt.Errorf("radio name is required")
	}
	if strings.TrimSpace(r.Model) == "" {
		return fmt.Errorf("radio model is required")
	}
	if _, err := ParsePowerIntent(string(r.DefaultPower)); err != nil {
		return err
	}
	switch r.Dedupe {
	case "", "none", "index", "freq":
	default:
		return fmt.Errorf("unknown dedupe %q; use none, index, or freq", r.Dedupe)
	}
	return nil
}

// SortChannels orders channels by name (the order they're listed in, so it
// leaves them be), freq (receive frequency), or group (by group name,
// ungrouped last). The sort is stable.
func SortChannels(cs []Channel, by string) error {
	switch by {
	case "name":
	case "freq":
		slices.SortStableFunc(cs, func(a, b Channel) int { return cmp.Compare(a.ReceiveHz, b.ReceiveHz) })
	case "group":
		key := func(c Channel) string {
			if c.Group == nil {
				return "￿"
			}
			return c.Group.Name
		}
		slices.SortStableFunc(cs, func(a, b Channel) int { return cmp.Compare(key(a), key(b)) })
	default:
		return fmt.Errorf("unknown sort %q; use name, freq, or group", by)
	}
	return nil
}
