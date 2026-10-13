package domain

import (
	"fmt"
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
	Dedupe string
	// Sync is a saved filter; `radio sync` appends channels matching it.
	Sync      RadioFilter
	Notes     string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// RadioFilter selects channels for a radio. Empty fields don't filter;
// a filter with every field empty matches nothing.
type RadioFilter struct {
	Tag     string
	GroupID string
	Service Service
}

func (f RadioFilter) Empty() bool { return f.Tag == "" && f.GroupID == "" && f.Service == "" }

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
	_, err := ParseService(string(r.Sync.Service))
	return err
}
