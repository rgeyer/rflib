package app

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ryan/rflib/internal/domain"
	"github.com/ryan/rflib/internal/model"
	"github.com/ryan/rflib/internal/program"
	"github.com/ryan/rflib/internal/store"
)

// A radio revision is the saved radio as it was at one time, saved by hand
// or by an export when the radio differs from its last revision. The
// latest exported revision is what the radio was last programmed with, so
// a radio that differs from it needs programming again.

// RadioRevisions returns a radio's revisions, oldest first.
func (s *Service) RadioRevisions(ctx context.Context, ref string) (domain.Radio, []domain.RadioRevision, error) {
	r, err := s.SavedRadio(ctx, ref)
	if err != nil {
		return r, nil, err
	}
	revs, err := s.lib.RadioRevisions(ctx, r.ID)
	return r, revs, err
}

// SaveRadioRevision saves the saved radio as a revision, with reason. It's
// refused when nothing changed since the last revision.
func (s *Service) SaveRadioRevision(ctx context.Context, ref, reason string) (domain.RadioRevision, error) {
	if err := s.writable(); err != nil {
		return domain.RadioRevision{}, err
	}
	r, revs, err := s.RadioRevisions(ctx, ref)
	if err != nil {
		return domain.RadioRevision{}, err
	}
	snap, err := s.lib.RadioSnapshot(ctx, r.ID)
	if err != nil {
		return domain.RadioRevision{}, err
	}
	if last := latest(revs, false); last != nil && sameSnapshot(last.Snapshot, snap) {
		return *last, domain.Invalidf("nothing changed on %s since revision %d", r.Name, last.Number)
	}
	return s.lib.SaveRadioRevision(store.WithChange(ctx, reason, ""), r.ID, snap, false)
}

// ExportReason is the reason an export's revision gets when none is
// given: the day, and that it was saved by an export from where.
func ExportReason(from string) string {
	return fmt.Sprintf("%s: saved automatically by %s export", time.Now().Format(time.DateOnly), from)
}

// RecordExport notes that the saved radio was exported: when it differs
// from its last revision, it's saved as a new one, with reason, marked
// exported; otherwise the last revision is marked exported. saved says
// which.
func (s *Service) RecordExport(ctx context.Context, ref, reason string) (rev domain.RadioRevision, saved bool, err error) {
	if err := s.writable(); err != nil {
		return rev, false, err
	}
	r, revs, err := s.RadioRevisions(ctx, ref)
	if err != nil {
		return rev, false, err
	}
	snap, err := s.lib.RadioSnapshot(ctx, r.ID)
	if err != nil {
		return rev, false, err
	}
	if last := latest(revs, false); last != nil && sameSnapshot(last.Snapshot, snap) {
		last.Exported = true
		return *last, false, s.lib.MarkRadioRevisionExported(ctx, r.ID, last.Number)
	}
	rev, err = s.lib.SaveRadioRevision(store.WithChange(ctx, reason, ""), r.ID, snap, true)
	return rev, err == nil, err
}

// RevisionState is a saved radio against its revisions: its latest
// revision (0 when none), whether it changed since (so an export would
// save a revision), and whether it differs from its latest exported
// revision, so needs programming again. A radio never exported needs
// programming once it has memories.
type RevisionState struct {
	Latest           int
	Changed          bool
	NeedsProgramming bool
}

// RevisionState returns a saved radio's RevisionState.
func (s *Service) RevisionState(ctx context.Context, ref string) (RevisionState, error) {
	var st RevisionState
	r, revs, err := s.RadioRevisions(ctx, ref)
	if err != nil {
		return st, err
	}
	snap, err := s.lib.RadioSnapshot(ctx, r.ID)
	if err != nil {
		return st, err
	}
	last, exported := latest(revs, false), latest(revs, true)
	if last != nil {
		st.Latest = last.Number
	}
	st.Changed = last == nil || !sameSnapshot(last.Snapshot, snap)
	if exported == nil {
		st.NeedsProgramming = len(snap.Memories) > 0
	} else {
		st.NeedsProgramming = !sameSnapshot(exported.Snapshot, snap)
	}
	return st, nil
}

// latest is the last revision, or the last exported one; nil when none.
func latest(revs []domain.RadioRevision, exported bool) *domain.RadioRevision {
	for i := len(revs) - 1; i >= 0; i-- {
		if !exported || revs[i].Exported {
			return &revs[i]
		}
	}
	return nil
}

func sameSnapshot(a, b domain.RadioSnapshot) bool {
	x, err1 := json.Marshal(a)
	y, err2 := json.Marshal(b)
	return err1 == nil && err2 == nil && string(x) == string(y)
}

// revision returns one of a radio's revisions by number.
func (s *Service) revision(ctx context.Context, ref string, n int) (domain.Radio, domain.RadioRevision, error) {
	r, revs, err := s.RadioRevisions(ctx, ref)
	if err != nil {
		return r, domain.RadioRevision{}, err
	}
	for _, rev := range revs {
		if rev.Number == n {
			return r, rev, nil
		}
	}
	return r, domain.RadioRevision{}, fmt.Errorf("%s has no revision %d: %w", r.Name, n, store.ErrNotFound)
}

// CompareRevision compares one of a radio's revisions with the radio as
// it is now (its open draft, if any): memories added, removed, moved, or
// renumbered since, other settings, channels whose own values changed, and
// sections and sources added or removed.
func (s *Service) CompareRevision(ctx context.Context, ref string, n int) (domain.RadioRevision, DraftChanges, error) {
	r, rev, err := s.revision(ctx, ref, n)
	if err != nil {
		return rev, DraftChanges{}, err
	}
	before, err := s.revisionList(ctx, r, rev.Snapshot)
	if err != nil {
		return rev, DraftChanges{}, err
	}
	now, _, err := s.ViewDraft(ctx, r.ID)
	if err != nil {
		return rev, DraftChanges{}, err
	}
	after, err := s.currentList(ctx, now)
	if err != nil {
		return rev, DraftChanges{}, err
	}
	return rev, diffLists(before, after), nil
}

// revisionList is a revision's list, with each channel as it was then.
func (s *Service) revisionList(ctx context.Context, r domain.Radio, snap domain.RadioSnapshot) (list, error) {
	var out list
	secs := make([]domain.RadioSection, len(snap.Sections))
	byID := map[string]domain.RadioSection{}
	for i, x := range snap.Sections {
		secs[i] = domain.RadioSection{ID: x.ID, RadioID: r.ID, Position: i, Name: x.Name, Start: x.Start, Size: x.Size, Sort: x.Sort, Default: x.Default}
		byID[x.ID] = secs[i]
		out.sections = append(out.sections, x.Name)
	}
	match := map[string]bool{}
	for i, x := range snap.Sources {
		src := domain.RadioSource{Position: i, Query: x.Query, PlanID: x.PlanID, PlanName: x.PlanID}
		if x.PlanID != "" {
			if p, err := s.GetPlan(ctx, x.PlanID); err == nil {
				src.PlanName = p.Name
			}
		}
		out.sources = append(out.sources, SourceLabel(src))
		match[x.ID] = x.PlanNumbers == "match"
	}
	for _, m := range snap.Memories {
		c, err := s.lib.ChannelAt(ctx, m.ChannelID, m.ChannelRevision)
		if err != nil {
			return out, err
		}
		if now, err := s.lib.GetChannel(ctx, m.ChannelID); err == nil && now.Link != nil {
			tags, notes, short, err := s.lib.NotesAt(ctx, m.ChannelID, m.NotesRevision)
			if err != nil {
				return out, err
			}
			c = domain.Linked(domain.Channel{Tags: tags, Notes: notes, ShortNames: short}, c, now.Link.SeenRevision)
		}
		out.entries = append(out.entries, domain.RadioEntry{Channel: c, Position: len(out.entries), Section: byID[m.SectionID],
			Name: m.Name, Power: m.Power, Skip: m.Skip, SourceID: m.SourceID, PlanNumber: m.PlanNumber, MatchPlan: match[m.SourceID]})
	}
	radio := domain.Radio{ID: r.ID, Name: snap.Name, Model: snap.Model, DefaultPower: snap.DefaultPower, Dedupe: snap.Dedupe, NameLength: snap.NameLength}
	out.numbers = make([]int, len(out.entries))
	for i := range out.numbers {
		out.numbers[i] = program.NoNumber
	}
	if m, err := model.Find(snap.Model); err == nil {
		if p, err := program.Build(radio, m, out.entries, secs...); err == nil {
			for i := range out.numbers {
				if i < len(p.Memories) {
					out.numbers[i] = p.Memories[i].Location
				}
			}
		}
	}
	return out, nil
}

// RestoreRevision puts one of a radio's revisions into its draft, to
// review and save or discard: its sections, sources, and memories (not its
// settings, which save at once). Channels are as they are now, since they
// belong to the library. With a draft open, it's refused unless
// replaceDraft.
func (s *Service) RestoreRevision(ctx context.Context, ref string, n int, replaceDraft bool) (domain.Radio, error) {
	if err := s.writable(); err != nil {
		return domain.Radio{}, err
	}
	r, rev, err := s.revision(ctx, ref, n)
	if err != nil {
		return r, err
	}
	_, d, err := s.Draft(ctx, r.ID)
	if err != nil {
		return r, err
	}
	if d != nil && !replaceDraft {
		return r, fmt.Errorf("%s already has a draft (started %s); restoring replaces it: %w", r.Name, started(*d), store.ErrConflict)
	}
	var draft domain.Radio
	err = s.lib.InTx(ctx, func(tx store.Library) error {
		if d != nil {
			if err := tx.DiscardRadioDraft(ctx, r.ID); err != nil {
				return err
			}
		}
		if draft, err = tx.StartRadioDraft(ctx, r.ID); err != nil {
			return err
		}
		return tx.ReplaceRadioList(ctx, draft.ID, rev.Snapshot)
	})
	return draft, err
}
