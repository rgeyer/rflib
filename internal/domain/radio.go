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
	// DraftOf is, for a radio's draft, the radio's ID (see
	// store.Library.StartRadioDraft); a draft has its radio's name and
	// settings.
	DraftOf string
	// DefaultPower applies to channels without a power intent of their own.
	DefaultPower PowerIntent
	// Dedupe is how duplicate names on this radio are told apart: none,
	// index, or freq (see package naming).
	Dedupe string
	// NameLength overrides the model's name length when set, for a radio
	// whose model doesn't know it (such as chirp-generic); 0 uses the
	// model's.
	NameLength int
	Notes      string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// RadioSection is a named part of a radio's list. Sections come in order;
// every memory belongs to one. A section starts at Start, or right after
// the section before it; Size, when set, reserves that many memories, so
// the next section without a start of its own begins after them (a
// section holding more spills over, with a warning). Its memories are in
// the order they're placed (Sort empty), or sorted by name or freq. A
// radio has one default section, where channels added by hand go.
type RadioSection struct {
	ID       string
	RadioID  string
	Position int
	Name     string
	Start    *int
	Size     *int
	Sort     string
	Default  bool
	// Origin is, for a section of a radio's draft, the ID of the radio's
	// section it's a copy of; empty for one the draft added.
	Origin string
}

// SectionSorts are a section's orders: manual (empty), name, or freq.
var SectionSorts = []string{"", "name", "freq"}

func ValidateRadioSection(sec RadioSection) error {
	if strings.TrimSpace(sec.Name) == "" {
		return Invalidf("a section needs a name")
	}
	if sec.Start != nil && *sec.Start < 0 {
		return Invalidf("a section's start must not be negative")
	}
	if sec.Size != nil && *sec.Size < 1 {
		return Invalidf("a section's size must be at least 1")
	}
	if !slices.Contains(SectionSorts, sec.Sort) {
		return Invalidf("unknown section order %q; use manual, name, or freq", sec.Sort)
	}
	return nil
}

// Plan numbering on a radio: compact numbers a plan's channels in plan
// order without gaps; match keeps the plan's numbers, memory = the
// section's first memory + Ch # - 1.
const (
	PlanNumbersCompact = "compact"
	PlanNumbersMatch   = "match"
)

// RadioSource is one of a radio's searches, or a plan, feeding a section.
// For a search, `radio sync` adds the channels it matches that aren't on
// the radio yet to the source's section, in Sort order (a sorted section
// keeps its own order). For a plan, the source's channels follow the plan:
// its channels, order, and names.
type RadioSource struct {
	ID      string
	RadioID string
	// Position orders a radio's sources, from 0. Earlier sources claim a
	// channel first.
	Position int
	// SectionID is the section the source feeds; SectionName is filled in
	// on reads.
	SectionID, SectionName string
	// Query is the search, in package query's language; empty for a plan.
	Query string
	// PlanID is the plan, from the shared library; empty for a search.
	// PlanName is filled in on reads.
	PlanID, PlanName string
	// Sort orders the channels a sync adds: name or freq.
	Sort string
	// PlanNumbers is how a plan's numbers carry onto the radio: compact
	// (the default) or match.
	PlanNumbers string
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
	switch src.PlanNumbers {
	case "", PlanNumbersCompact, PlanNumbersMatch:
	default:
		return Invalidf("unknown plan numbering %q; use compact or match", src.PlanNumbers)
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
	// Position is the entry's index in the radio's list, from 0, with the
	// list in section order.
	Position int
	// Section is the section the entry is in, filled in on reads.
	Section RadioSection
	Name    string
	Power   PowerIntent
	Skip    *bool
	// SourceID is the radio source that added the entry; empty for
	// channels added by hand. SourceNumber is that source's number (from
	// 1), filled in on reads.
	SourceID     string
	SourceNumber int
	// PlanNumber is a plan source's Ch # for the entry; with MatchPlan
	// (the source matches plan numbers), the entry goes at its section's
	// first memory + PlanNumber - 1.
	PlanNumber *int
	MatchPlan  bool
}

// MaxRadioNameLength bounds a radio's name length override.
const MaxRadioNameLength = 64

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
	if r.NameLength < 0 || r.NameLength > MaxRadioNameLength {
		return Invalidf("name length must be 0 (the model's) to %d", MaxRadioNameLength)
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
