package app

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/ryan/rflib/internal/domain"
	"github.com/ryan/rflib/internal/program"
	"github.com/ryan/rflib/internal/store"
)

// A radio's draft holds changes to its list (its sections, sources, and
// channels) until they're saved or discarded; settings save at once. A
// radio has at most one draft, shared by the web and the CLI: commands that
// change the list work on it, and those that read the list read it while
// it's open. Exports, copies, and other radios' comparisons use the saved
// radio.

// ErrNoDraft is a change to a radio's list with no draft open.
var ErrNoDraft = errors.New("no draft")

// ErrStaleDraft is a change made against a radio's draft (or its saved
// list) that's no longer the one open: the page it came from is out of
// date.
var ErrStaleDraft = errors.New("draft out of date")

// DraftExpectation says, for a change, how to find a radio's draft: with
// Auto, the change opens one when none is open (the web's first change);
// with Seen, it's refused unless the open draft is the one seen ("" for
// none).
type DraftExpectation struct {
	Auto bool
	Seen *string
}

type draftKey struct{}

// WithDraftExpectation returns ctx carrying how changes find a radio's
// draft (see DraftExpectation). Without one, a change needs a draft open.
func WithDraftExpectation(ctx context.Context, e DraftExpectation) context.Context {
	return context.WithValue(ctx, draftKey{}, e)
}

// SavedRadio returns a radio, never its draft: by name, or by its own or
// its draft's ID.
func (s *Service) SavedRadio(ctx context.Context, ref string) (domain.Radio, error) {
	r, err := s.lib.GetRadio(ctx, ref)
	if err != nil || r.DraftOf == "" {
		return r, err
	}
	return s.lib.GetRadio(ctx, r.DraftOf)
}

// Draft returns a radio and its open draft, if any.
func (s *Service) Draft(ctx context.Context, ref string) (domain.Radio, *domain.Radio, error) {
	r, err := s.SavedRadio(ctx, ref)
	if err != nil {
		return r, nil, err
	}
	d, err := s.lib.RadioDraft(ctx, r.ID)
	if errors.Is(err, store.ErrNotFound) {
		return r, nil, nil
	}
	if err != nil {
		return r, nil, err
	}
	return r, &d, nil
}

// StartDraft opens a radio's draft, a copy of its list as it is now.
func (s *Service) StartDraft(ctx context.Context, ref string) (domain.Radio, error) {
	if err := s.writable(); err != nil {
		return domain.Radio{}, err
	}
	r, d, err := s.Draft(ctx, ref)
	if err != nil {
		return r, err
	}
	if d != nil {
		return *d, fmt.Errorf("%s already has a draft (started %s): %w", r.Name, started(*d), store.ErrConflict)
	}
	return s.lib.StartRadioDraft(ctx, r.ID)
}

// SaveDraft makes a radio's list its draft's and closes the draft.
func (s *Service) SaveDraft(ctx context.Context, ref string) (domain.Radio, error) {
	r, err := s.closeDraft(ctx, ref)
	if err != nil {
		return r, err
	}
	if err := s.lib.SaveRadioDraft(ctx, r.ID); err != nil {
		return r, err
	}
	return s.lib.GetRadio(ctx, r.ID)
}

// DiscardDraft closes a radio's draft, leaving the radio as it was.
func (s *Service) DiscardDraft(ctx context.Context, ref string) (domain.Radio, error) {
	r, err := s.closeDraft(ctx, ref)
	if err != nil {
		return r, err
	}
	return r, s.lib.DiscardRadioDraft(ctx, r.ID)
}

// closeDraft checks a radio's draft can be saved or discarded: it's open,
// and, when ctx says which draft was seen, it's that one.
func (s *Service) closeDraft(ctx context.Context, ref string) (domain.Radio, error) {
	if err := s.writable(); err != nil {
		return domain.Radio{}, err
	}
	r, d, err := s.Draft(ctx, ref)
	if err != nil {
		return r, err
	}
	if d == nil {
		return r, fmt.Errorf("%s has no draft: %w", r.Name, ErrNoDraft)
	}
	if e, ok := ctx.Value(draftKey{}).(DraftExpectation); ok && e.Seen != nil && *e.Seen != d.ID {
		return r, staleDraft(r, d)
	}
	return r, nil
}

// EditDraft returns the ID of the radio's draft for a change to its list,
// as ctx says (see DraftExpectation): the open draft, or a new one when ctx
// allows it.
func (s *Service) EditDraft(ctx context.Context, ref string) (string, error) {
	if err := s.writable(); err != nil {
		return "", err
	}
	r, d, err := s.Draft(ctx, ref)
	if err != nil {
		return "", err
	}
	e, _ := ctx.Value(draftKey{}).(DraftExpectation)
	seen := ""
	if d != nil {
		seen = d.ID
	}
	if e.Seen != nil && *e.Seen != seen {
		return "", staleDraft(r, d)
	}
	if d != nil {
		return d.ID, nil
	}
	if !e.Auto {
		return "", fmt.Errorf("%s has no draft; start one first: %w", r.Name, ErrNoDraft)
	}
	nd, err := s.lib.StartRadioDraft(ctx, r.ID)
	if err != nil {
		return "", err
	}
	return nd.ID, nil
}

// ViewDraft returns the ID to read a radio's list from: its draft's while
// one is open, else its own.
func (s *Service) ViewDraft(ctx context.Context, ref string) (string, bool, error) {
	r, d, err := s.Draft(ctx, ref)
	if err != nil {
		return "", false, err
	}
	if d != nil {
		return d.ID, true, nil
	}
	return r.ID, false, nil
}

func staleDraft(r domain.Radio, d *domain.Radio) error {
	if d == nil {
		return fmt.Errorf("%s's draft was saved or discarded; showing the radio as it is: %w", r.Name, ErrStaleDraft)
	}
	return fmt.Errorf("%s already has a draft (started %s); showing it: %w", r.Name, started(*d), ErrStaleDraft)
}

// started is when a draft was opened, in local time.
func started(d domain.Radio) string {
	return d.CreatedAt.Local().Format("2006-01-02 15:04")
}

// DraftChanges is what a radio's draft changes: memories added, removed,
// moved to another section, renumbered within theirs, or with other
// settings (name, power, or scan), and sections and sources added or
// removed (by name, and by search).
type DraftChanges struct {
	Added, Removed, Changed []domain.Channel
	Moved, Renumbered       []MovedMemory
	SectionsAdded           []string
	SectionsRemoved         []string
	SourcesAdded            []string
	SourcesRemoved          []string
}

// Empty reports whether the draft changes nothing.
func (c DraftChanges) Empty() bool {
	return len(c.Added)+len(c.Removed)+len(c.Changed)+len(c.Moved)+len(c.Renumbered)+len(c.SectionsAdded)+len(c.SectionsRemoved)+len(c.SourcesAdded)+len(c.SourcesRemoved) == 0
}

// MovedMemory is a memory the draft renumbers or moves to another section:
// where it was, and where it is in the draft. A number is
// program.NoNumber for a memory that isn't programmed.
type MovedMemory struct {
	Channel        domain.Channel
	From, To       int
	FromSec, ToSec string
}

// CompareDraft returns a radio's draft (nil when none is open), and what
// it changes.
func (s *Service) CompareDraft(ctx context.Context, ref string) (domain.Radio, *domain.Radio, DraftChanges, error) {
	var out DraftChanges
	r, d, err := s.Draft(ctx, ref)
	if err != nil || d == nil {
		return r, d, out, err
	}
	type memory struct {
		entry  domain.RadioEntry
		number int
	}
	memories := func(id string) (map[string]memory, []string, error) {
		p, es, err := s.Layout(ctx, id)
		if err != nil {
			// A list that doesn't lay out still compares, without numbers.
			if es, err = s.lib.RadioEntries(ctx, id); err != nil {
				return nil, nil, err
			}
			p = program.Layout{}
		}
		m := make(map[string]memory, len(es))
		order := make([]string, len(es))
		for i, e := range es {
			n := program.NoNumber
			if i < len(p.Memories) {
				n = p.Memories[i].Location
			}
			m[e.Channel.ID] = memory{e, n}
			order[i] = e.Channel.ID
		}
		return m, order, nil
	}
	before, beforeOrder, err := memories(r.ID)
	if err != nil {
		return r, d, out, err
	}
	after, afterOrder, err := memories(d.ID)
	if err != nil {
		return r, d, out, err
	}
	for _, id := range afterOrder {
		a := after[id]
		b, ok := before[id]
		switch {
		case !ok:
			out.Added = append(out.Added, a.entry.Channel)
		case a.entry.Section.Name != b.entry.Section.Name:
			out.Moved = append(out.Moved, MovedMemory{Channel: a.entry.Channel, From: b.number, To: a.number, FromSec: b.entry.Section.Name, ToSec: a.entry.Section.Name})
		case a.number != b.number:
			out.Renumbered = append(out.Renumbered, MovedMemory{Channel: a.entry.Channel, From: b.number, To: a.number, FromSec: b.entry.Section.Name, ToSec: a.entry.Section.Name})
		case a.entry.Name != b.entry.Name || a.entry.Power != b.entry.Power || !sameSkip(a.entry.Skip, b.entry.Skip):
			out.Changed = append(out.Changed, a.entry.Channel)
		}
	}
	for _, id := range beforeOrder {
		if _, ok := after[id]; !ok {
			out.Removed = append(out.Removed, before[id].entry.Channel)
		}
	}
	names := func(id string) ([]string, []string, error) {
		secs, err := s.lib.RadioSections(ctx, id)
		if err != nil {
			return nil, nil, err
		}
		srcs, err := s.lib.RadioSources(ctx, id)
		if err != nil {
			return nil, nil, err
		}
		var sn, qn []string
		for _, x := range secs {
			sn = append(sn, x.Name)
		}
		for _, x := range srcs {
			qn = append(qn, SourceLabel(x))
		}
		return sn, qn, nil
	}
	bs, bq, err := names(r.ID)
	if err != nil {
		return r, d, out, err
	}
	as, aq, err := names(d.ID)
	if err != nil {
		return r, d, out, err
	}
	out.SectionsAdded, out.SectionsRemoved = missing(as, bs), missing(bs, as)
	out.SourcesAdded, out.SourcesRemoved = missing(aq, bq), missing(bq, aq)
	return r, d, out, nil
}

// missing returns what a has and b doesn't, in a's order.
func missing(a, b []string) []string {
	var out []string
	for _, x := range a {
		if !slices.Contains(b, x) {
			out = append(out, x)
		}
	}
	return out
}

func sameSkip(a, b *bool) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}

// DraftStarted is when a radio's open draft was started; zero when none
// is.
func DraftStarted(d *domain.Radio) time.Time {
	if d == nil {
		return time.Time{}
	}
	return d.CreatedAt
}

// radioIDNames maps the IDs of the library's radios, and of their open
// drafts, to the radios' names.
func (s *Service) radioIDNames(ctx context.Context) (map[string]string, error) {
	rs, err := s.lib.ListRadios(ctx)
	if err != nil {
		return nil, err
	}
	names := make(map[string]string, len(rs))
	for _, r := range rs {
		names[r.ID] = r.Name
		d, err := s.lib.RadioDraft(ctx, r.ID)
		if err == nil {
			names[d.ID] = r.Name
		} else if !errors.Is(err, store.ErrNotFound) {
			return nil, err
		}
	}
	return names, nil
}
