package domain

import (
	"cmp"
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
	// DefaultPower applies to channels without a power intent of their own.
	DefaultPower PowerIntent
	// Dedupe is how duplicate names on this radio are told apart: none,
	// index, or freq (see package naming).
	Dedupe    string
	Notes     string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// RadioSource is one of a radio's searches, or a plan. For a search, `radio
// sync` appends the channels it matches that aren't on the radio yet to the
// source's part of the list, in Sort order. For a plan, the source's part
// of the list follows the plan: its channels, order, and names.
type RadioSource struct {
	ID      string
	RadioID string
	// Position orders a radio's sources, from 0. Earlier sources claim a
	// channel first.
	Position int
	// Query is the search, in package query's language; empty for a plan.
	Query string
	// PlanID is the plan, from the shared library; empty for a search.
	// PlanName is filled in on reads.
	PlanID, PlanName string
	// Sort orders the channels a sync adds: name or freq.
	Sort string
	// SectionStart, when set, starts the source's first channel at that
	// memory number.
	SectionStart *int
	// KeepSorted re-sorts the source's channels on every sync, as
	// `radio sync --resort` does.
	KeepSorted bool
}

// SortOrders are the orders channels can be added in.
var SortOrders = []string{"name", "freq"}

func ValidateRadioSource(src RadioSource) error {
	if (strings.TrimSpace(src.Query) == "") == (src.PlanID == "") {
		return Invalidf("a source needs a query or a plan")
	}
	if !slices.Contains(SortOrders, src.Sort) {
		return sortError(src.Sort)
	}
	if src.SectionStart != nil && *src.SectionStart < 0 {
		return Invalidf("section start must not be negative")
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
		return Invalidf("search name %q must be lower-case letters, digits, -, _, and ., starting with a letter or digit", s.Name)
	}
	if strings.TrimSpace(s.Query) == "" {
		return Invalidf("a saved search needs a query")
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
				return nil, Invalidf("%q starts a section at memory %d, but the channels before it already reach memory %d", e.Channel.Name, *e.SectionStart, next-1)
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
		return Invalidf("radio name is required")
	}
	if strings.TrimSpace(r.Model) == "" {
		return Invalidf("radio model is required")
	}
	if _, err := ParsePowerIntent(string(r.DefaultPower)); err != nil {
		return err
	}
	switch r.Dedupe {
	case "", "none", "index", "freq":
	default:
		return Invalidf("unknown dedupe %q; use none, index, or freq", r.Dedupe)
	}
	return nil
}

// SortChannels orders channels by name (see CompareChannels) or freq
// (receive frequency). The sort is stable.
func SortChannels(cs []Channel, by string) error {
	switch by {
	case "name":
		slices.SortStableFunc(cs, CompareChannels)
	case "freq":
		slices.SortStableFunc(cs, func(a, b Channel) int { return cmp.Compare(a.ReceiveHz, b.ReceiveHz) })
	default:
		return sortError(by)
	}
	return nil
}

func sortError(by string) error {
	if by == "group" {
		return Invalidf("sorting by group is gone with groups; use name or freq")
	}
	return Invalidf("unknown sort %q; use name or freq", by)
}

// CompareChannels orders channels by name (see CompareNames), then by
// receive frequency.
func CompareChannels(a, b Channel) int {
	return cmp.Or(CompareNames(a.Name, b.Name), cmp.Compare(a.ReceiveHz, b.ReceiveHz))
}

// CompareNames orders names with their runs of digits compared as numbers,
// so "GMRS 2" comes before "GMRS 10" and "GMRS 15" before "GMRS 15R". The
// rest compares byte by byte. Of numbers equal in value, the one with fewer
// leading zeros comes first.
func CompareNames(a, b string) int {
	zeros := 0 // the first difference in leading zeros, for a tie
	for a != "" && b != "" {
		da, db := digitPrefix(a), digitPrefix(b)
		if da == 0 || db == 0 {
			if c := cmp.Compare(a[0], b[0]); c != 0 {
				return c
			}
			a, b = a[1:], b[1:]
			continue
		}
		na, nb := strings.TrimLeft(a[:da], "0"), strings.TrimLeft(b[:db], "0")
		if c := cmp.Or(cmp.Compare(len(na), len(nb)), cmp.Compare(na, nb)); c != 0 {
			return c
		}
		zeros = cmp.Or(zeros, cmp.Compare(da, db))
		a, b = a[da:], b[db:]
	}
	return cmp.Or(cmp.Compare(len(a), len(b)), zeros)
}

func digitPrefix(s string) int {
	n := 0
	for n < len(s) && s[n] >= '0' && s[n] <= '9' {
		n++
	}
	return n
}
