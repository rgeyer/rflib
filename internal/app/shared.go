package app

import (
	"context"
	"crypto/sha1"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ryan/rflib/internal/domain"
	"github.com/ryan/rflib/internal/export"
	"github.com/ryan/rflib/internal/importer"
	"github.com/ryan/rflib/internal/query"
	"github.com/ryan/rflib/internal/store"
)

// The shared library holds channels shared with everyone. A library sees a
// shared channel by its shared ID, with the user's annotation (their own
// tags, notes, and short names) laid over it, and its radios hold shared
// channels directly. A library follows the shared channels it annotates or
// has on a radio, for Changes. Everyday commands can't change the shared
// library; the operations here marked as maintainer operations do, and
// record a revision for each change.

// sharedLibrary returns the shared library.
func (s *Service) sharedLibrary(ctx context.Context) (store.Library, error) {
	sh, err := s.shared(ctx)
	if err != nil {
		return nil, err
	}
	return sh.lib, nil
}

// shared returns the Service of the shared library.
func (s *Service) shared(ctx context.Context) (*Service, error) {
	if s.info.Shared {
		return s, nil
	}
	if s.st == nil {
		return nil, errors.New("the shared library isn't available here")
	}
	return Open(ctx, s.st, domain.SharedLibrary)
}

// LinkedEdit refuses a change to a shared channel other than to the user's
// annotation: its tags, notes, and short names. Power and skip are set per
// radio. old is the view as stored, c as it would be saved.
func LinkedEdit(old, c domain.Channel) error {
	if old.Link == nil || c.Link == nil || old.Link.SharedID != c.Link.SharedID {
		return nil
	}
	if c.Power != old.Power || c.Skip != old.Skip {
		return domain.Invalidf("%s is a shared channel, so its power and scan are set on each radio (rflib radio channels set)", old.Name)
	}
	a, b := old, c
	a.Tags, a.Notes, b.Tags, b.Notes = nil, "", nil, ""
	if changes := domain.SharedChanges(a, b); len(changes) > 0 {
		field, _, _ := strings.Cut(changes[0], ":")
		return domain.Invalidf("%s is a shared channel, so its %s can't change here; make it a private copy with rflib channel copy --id %s", old.Name, field, old.ID)
	}
	return nil
}

// scoped returns the channels an in: scope covers, as the library sees
// them (shared channels with its annotations), in name order:
//
//   - use (the default for lists): the library's own channels, and the
//     shared channels it annotates or has on a radio
//   - mine: only the library's own channels
//   - shared: the whole shared list
//   - all: the library's own channels and the whole shared list
//
// forRadios leaves out shared channels the library has copied, since a copy
// stands in for its shared channel. In the shared library, every scope is
// its channels.
func (s *Service) scoped(ctx context.Context, scope string, forRadios bool) ([]domain.Channel, error) {
	mine, err := s.lib.ListChannels(ctx)
	if err != nil || s.info.Shared {
		return mine, err
	}
	var own []domain.Channel
	inUse := map[string]bool{}
	copied := map[string]bool{}
	for _, c := range mine {
		if c.Link != nil {
			inUse[c.ID] = true
			continue
		}
		own = append(own, c)
		if c.CopiedFrom != "" {
			copied[c.CopiedFrom] = true
		}
	}
	var ids []string
	switch scope {
	case "mine":
		return own, nil
	case "", "use":
		onRadios, err := s.radioNames(ctx)
		if err != nil {
			return nil, err
		}
		for id := range onRadios {
			inUse[id] = true
		}
		for id := range inUse {
			ids = append(ids, id)
		}
	default:
		sh, err := s.sharedLibrary(ctx)
		if err != nil {
			return nil, err
		}
		shared, err := sh.ListChannels(ctx)
		if err != nil {
			return nil, err
		}
		for _, c := range shared {
			if !forRadios || !copied[c.ID] {
				ids = append(ids, c.ID)
			}
		}
	}
	views, err := s.lib.SharedViews(ctx, ids)
	if err != nil {
		return nil, err
	}
	var out []domain.Channel
	if scope != "shared" {
		out = own
	}
	for _, v := range views {
		out = append(out, v)
	}
	slices.SortStableFunc(out, domain.CompareChannels)
	return out, nil
}

// FindForRadios returns the channels matching a query among those a radio
// can take, in name order: by default the library's own channels and the
// whole shared list, or the query's in: scope (see scoped).
func (s *Service) FindForRadios(ctx context.Context, n query.Node) ([]domain.Channel, error) {
	scope, n, err := query.SplitScope(n)
	if err != nil {
		return nil, err
	}
	m, err := query.Compile(n, s.QueryEnv(ctx))
	if err != nil {
		return nil, err
	}
	if scope == "" {
		scope = "all"
	}
	all, err := s.scoped(ctx, scope, true)
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

// Conversion is a private channel that matches a shared one, and the
// library's view of the shared channel it becomes.
type Conversion struct {
	Private, Shared, Link domain.Channel
}

// ConvertToLinks replaces private channels with the shared channels they
// match: the same frequencies, tones, and tone squelch
// (domain.ProgrammedKey), and the same service unless the private channel
// has none, since FRS and GMRS share frequencies. Of several matches, one
// with the same name wins, then an active one. The shared channel takes
// the private one's place on radios, keeping its overrides; the private
// channel's tags and notes, less what the shared channel already has, and
// a name that differs from the shared one, as a short name, become the
// user's annotation; and the private channel goes. only, when not nil,
// limits the shared channels considered, and ids, when not nil, limits the
// private channels. With dryRun nothing changes.
func (s *Service) ConvertToLinks(ctx context.Context, only map[string]bool, ids []string, dryRun bool) ([]Conversion, error) {
	if err := s.writable(); err != nil {
		return nil, err
	}
	sh, err := s.sharedLibrary(ctx)
	if err != nil {
		return nil, err
	}
	shared, err := sh.ListChannels(ctx)
	if err != nil {
		return nil, err
	}
	return s.convertTo(ctx, shared, only, ids, dryRun)
}

// convertTo is ConvertToLinks against the given shared channels.
func (s *Service) convertTo(ctx context.Context, shared []domain.Channel, only map[string]bool, ids []string, dryRun bool) ([]Conversion, error) {
	byKey := map[string][]domain.Channel{}
	for _, c := range shared {
		if only == nil || only[c.ID] {
			k := domain.ProgrammedKey(c)
			byKey[k] = append(byKey[k], c)
		}
	}
	mine, err := s.lib.ListChannels(ctx)
	if err != nil {
		return nil, err
	}
	views := map[string]domain.Channel{}
	for _, c := range mine {
		if c.Link != nil {
			views[c.ID] = c
		}
	}
	var out []Conversion
	for _, c := range mine {
		// A retired channel is done with, linked already or not.
		if c.Link != nil || c.Status.OrActive() == domain.StatusRetired || (ids != nil && !slices.Contains(ids, c.ID)) {
			continue
		}
		target, ok := bestMatch(c, byKey[domain.ProgrammedKey(c)])
		if !ok {
			continue
		}
		view, ok := views[target.ID]
		if !ok {
			view = domain.Linked(domain.Channel{}, target, target.Revision)
		}
		for _, t := range c.Tags {
			if !slices.Contains(target.Tags, t) && !slices.Contains(view.Tags, t) {
				view.Tags = append(view.Tags, t)
			}
		}
		if n := strings.TrimSpace(c.Notes); n != "" && n != strings.TrimSpace(target.Notes) && !strings.Contains(view.Notes, n) {
			view.Notes = strings.TrimSpace(strings.TrimSpace(view.Notes) + "\n" + n)
		}
		for _, name := range append([]string{c.Name}, c.ShortNames...) {
			if name != target.Name && !slices.Contains(view.ShortNames, name) {
				view.ShortNames = append(view.ShortNames, name)
			}
		}
		views[target.ID] = view
		out = append(out, Conversion{Private: c, Shared: target, Link: view})
	}
	if dryRun || len(out) == 0 {
		return out, nil
	}
	err = s.lib.InTx(ctx, func(tx store.Library) error {
		for i, cv := range out {
			saved, err := tx.SaveAnnotation(ctx, cv.Link)
			if err != nil {
				return fmt.Errorf("%s: %w", cv.Private.Name, err)
			}
			if err := tx.ReplaceOnRadios(ctx, cv.Private.ID, cv.Shared.ID); err != nil {
				return fmt.Errorf("%s: %w", cv.Private.Name, err)
			}
			// Retired, not deleted: it stays, pointing at the shared channel.
			p := cv.Private
			p.Status, p.StatusNote, p.BackOn, p.ReplacedBy = domain.StatusRetired, "linked to the shared channel", nil, cv.Shared.ID
			if _, err := tx.UpdateChannel(store.WithChange(ctx, "linked to the shared channel", ""), p); err != nil {
				return fmt.Errorf("%s: %w", cv.Private.Name, err)
			}
			out[i].Link = saved
		}
		return nil
	})
	return out, err
}

// bestMatch picks the shared channel a private one should become from
// those programmed the same, or reports none fits.
func bestMatch(c domain.Channel, candidates []domain.Channel) (domain.Channel, bool) {
	var best domain.Channel
	score := -1
	for _, t := range candidates {
		if c.Service != domain.ServiceUnspecified && t.Service != c.Service {
			continue
		}
		n := 0
		if t.Name == c.Name {
			n += 2
		}
		if t.Status.OrActive() == domain.StatusActive {
			n++
		}
		if n > score {
			best, score = t, n
		}
	}
	return best, score >= 0
}

// CopyChannels makes a private channel from each selected shared channel,
// showing what it showed, with the shared tags and notes made its own, and
// remembering it in CopiedFrom. The copy takes the shared channel's place
// on radios, and the user's annotation of it goes. Private channels in the
// selection are left alone.
func (s *Service) CopyChannels(ctx context.Context, sel Selection) ([]domain.Channel, error) {
	if err := s.writable(); err != nil {
		return nil, err
	}
	cs, err := s.Select(ctx, sel)
	if err != nil {
		return nil, err
	}
	var out []domain.Channel
	err = s.lib.InTx(ctx, func(tx store.Library) error {
		for _, c := range cs {
			if c.Link == nil {
				continue
			}
			saved, err := copyShared(ctx, tx, c)
			if err != nil {
				return err
			}
			out = append(out, saved)
		}
		return nil
	})
	return out, err
}

// copyShared makes a private copy of the shared channel c (see
// CopyChannels).
func copyShared(ctx context.Context, tx store.Library, c domain.Channel) (domain.Channel, error) {
	sharedID := c.Link.SharedID
	cp := c
	cp.ID, cp.Tags, cp.Notes, cp.CopiedFrom = "", c.AllTags(), c.AllNotes(), sharedID
	cp.Link, cp.Status, cp.StatusNote, cp.BackOn, cp.ReplacedBy, cp.Revision = nil, domain.StatusActive, "", nil, "", 0
	cp.Orgs, cp.Networks = nil, nil
	saved, err := tx.CreateChannel(ctx, cp)
	if err != nil {
		return saved, fmt.Errorf("%s: %w", c.Name, err)
	}
	if err := tx.ReplaceOnRadios(ctx, sharedID, saved.ID); err != nil {
		return saved, fmt.Errorf("%s: %w", c.Name, err)
	}
	if c.Annotated() {
		if err := tx.DeleteChannel(ctx, sharedID); err != nil {
			return saved, fmt.Errorf("%s: %w", c.Name, err)
		}
	}
	return saved, nil
}

// --- changes ---

// Change is a followed shared channel that changed since the user last
// acknowledged it: what changed, why, the radios it's on, and, for a
// retired channel, its replacement.
type Change struct {
	Channel     domain.Channel
	Changes     []string
	Reasons     []string
	Radios      []string
	Replacement *domain.Channel
}

// followed returns the library's views of the shared channels it follows:
// those it annotates or has on a radio.
func (s *Service) followed(ctx context.Context) ([]domain.Channel, map[string][]string, error) {
	onRadios, err := s.radioNames(ctx)
	if err != nil {
		return nil, nil, err
	}
	cs, err := s.lib.ListChannels(ctx)
	if err != nil {
		return nil, nil, err
	}
	seen := map[string]bool{}
	var out []domain.Channel
	for _, c := range cs {
		if c.Link != nil {
			out = append(out, c)
			seen[c.ID] = true
		}
	}
	var ids []string
	for id := range onRadios {
		if !seen[id] {
			ids = append(ids, id)
		}
	}
	views, err := s.lib.SharedViews(ctx, ids)
	if err != nil {
		return nil, nil, err
	}
	for _, v := range views {
		out = append(out, v)
	}
	slices.SortStableFunc(out, domain.CompareChannels)
	return out, onRadios, nil
}

// Changes lists the followed shared channels that changed.
func (s *Service) Changes(ctx context.Context) ([]Change, error) {
	cs, onRadios, err := s.followed(ctx)
	if err != nil {
		return nil, err
	}
	sh, err := s.sharedLibrary(ctx)
	if err != nil {
		return nil, err
	}
	var out []Change
	for _, c := range cs {
		if !c.Changed() {
			continue
		}
		revs, err := s.lib.ChannelRevisions(ctx, c.ID)
		if err != nil {
			return nil, err
		}
		ch := Change{Channel: c, Radios: onRadios[c.ID]}
		var seen *domain.Revision
		for i, r := range revs {
			if r.Number == c.Link.SeenRevision {
				seen = &revs[i]
			}
			if r.Number > c.Link.SeenRevision && r.Reason != "" {
				ch.Reasons = append(ch.Reasons, r.Reason)
			}
		}
		if seen != nil && len(revs) > 0 {
			ch.Changes = domain.SharedChanges(seen.Channel, revs[len(revs)-1].Channel)
		}
		if c.ReplacedBy != "" {
			if r, err := sh.GetChannel(ctx, c.ReplacedBy); err == nil {
				ch.Replacement = &r
			}
		}
		out = append(out, ch)
	}
	return out, nil
}

// radioNames maps each channel to the radios it's on, or on whose open
// drafts.
func (s *Service) radioNames(ctx context.Context) (map[string][]string, error) {
	names, err := s.radioIDNames(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(names))
	for id := range names {
		ids = append(ids, id)
	}
	slices.SortFunc(ids, func(a, b string) int { return strings.Compare(names[a], names[b]) })
	out := map[string][]string{}
	for _, rid := range ids {
		on, err := s.lib.RadioChannelIDs(ctx, rid)
		if err != nil {
			return nil, err
		}
		for id := range on {
			if !slices.Contains(out[id], names[rid]) {
				out[id] = append(out[id], names[rid])
			}
		}
	}
	return out, nil
}

// changedSelection returns the selected followed shared channels that
// changed. The selection's IDs may name any followed channel; a query or
// All picks among them.
func (s *Service) changedSelection(ctx context.Context, sel Selection) ([]domain.Channel, error) {
	cs, _, err := s.followed(ctx)
	if err != nil {
		return nil, err
	}
	var m query.Matcher
	if sel.Query != nil {
		if m, err = query.Compile(sel.Query, s.QueryEnv(ctx)); err != nil {
			return nil, err
		}
	}
	ids, err := s.ChannelIDs(ctx, sel.IDs)
	if err != nil {
		return nil, err
	}
	var out []domain.Channel
	for _, c := range cs {
		switch {
		case !c.Changed():
		case len(ids) > 0 && !slices.Contains(ids, c.ID):
		case m != nil && !m(c):
		case len(ids) == 0 && m == nil && !sel.All:
		default:
			out = append(out, c)
		}
	}
	return out, nil
}

// Acknowledge marks the selected followed shared channels' changes as
// seen, and returns those that had changes.
func (s *Service) Acknowledge(ctx context.Context, sel Selection) ([]domain.Channel, error) {
	if err := s.writable(); err != nil {
		return nil, err
	}
	cs, err := s.changedSelection(ctx, sel)
	if err != nil {
		return nil, err
	}
	err = s.lib.InTx(ctx, func(tx store.Library) error {
		for i, c := range cs {
			if err := tx.MarkSeen(ctx, c.ID, c.Revision); err != nil {
				return err
			}
			cs[i].Link.SeenRevision = c.Revision
		}
		return nil
	})
	return cs, err
}

// Relink moves each selected retired shared channel's place on radios, and
// the user's annotation, to its replacement, and returns the replacements
// as the library now sees them.
func (s *Service) Relink(ctx context.Context, sel Selection) ([]domain.Channel, error) {
	if err := s.writable(); err != nil {
		return nil, err
	}
	sh, err := s.sharedLibrary(ctx)
	if err != nil {
		return nil, err
	}
	cs, err := s.changedSelection(ctx, sel)
	if err != nil {
		return nil, err
	}
	next := map[string]domain.Channel{}
	for _, c := range cs {
		if c.Status.OrActive() == domain.StatusRetired && c.ReplacedBy != "" {
			r, err := sh.GetChannel(ctx, c.ReplacedBy)
			if err != nil {
				return nil, fmt.Errorf("%s: replacement: %w", c.Name, err)
			}
			next[c.ID] = r
		}
	}
	var out []domain.Channel
	err = s.lib.InTx(ctx, func(tx store.Library) error {
		for _, c := range cs {
			r, ok := next[c.ID]
			if !ok {
				continue
			}
			saved, err := relinkShared(ctx, tx, c, r)
			if err != nil {
				return err
			}
			out = append(out, saved)
		}
		return nil
	})
	return out, err
}

// relinkShared moves a retired shared channel's places on the library's
// radios, and the user's annotation, to its replacement r, and marks the
// change seen.
func relinkShared(ctx context.Context, tx store.Library, c, r domain.Channel) (domain.Channel, error) {
	if err := tx.ReplaceOnRadios(ctx, c.ID, r.ID); err != nil {
		return r, fmt.Errorf("%s: %w", c.Name, err)
	}
	saved, err := tx.SaveAnnotation(ctx, domain.Linked(c, r, r.Revision))
	if err != nil {
		return r, fmt.Errorf("%s: %w", c.Name, err)
	}
	if c.Annotated() {
		if err := tx.DeleteChannel(ctx, c.ID); err != nil {
			return r, fmt.Errorf("%s: %w", c.Name, err)
		}
	}
	return saved, tx.MarkSeen(ctx, c.ID, c.Revision)
}

// Relinked is a retired channel moved over to its replacement.
type Relinked struct {
	From, To domain.Channel
}

// RelinkChannels moves each selected retired channel that names a
// replacement over to it: its places on the library's radios, keeping
// their overrides, and, for a shared channel, the user's tags and notes
// (as Relink does). It returns what moved, and the selected channels left
// as they were, not being retired with a replacement.
func (s *Service) RelinkChannels(ctx context.Context, sel Selection) ([]Relinked, []domain.Channel, error) {
	if err := s.writable(); err != nil {
		return nil, nil, err
	}
	cs, err := s.Select(ctx, sel)
	if err != nil {
		return nil, nil, err
	}
	var moves []Relinked
	var left []domain.Channel
	for _, c := range cs {
		if c.Status.OrActive() != domain.StatusRetired || c.ReplacedBy == "" {
			left = append(left, c)
			continue
		}
		r, err := s.lib.GetChannel(ctx, c.ReplacedBy)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: replacement: %w", c.Name, err)
		}
		moves = append(moves, Relinked{From: c, To: r})
	}
	err = s.lib.InTx(ctx, func(tx store.Library) error {
		for i, m := range moves {
			if m.From.Link != nil {
				saved, err := relinkShared(ctx, tx, m.From, m.To)
				if err != nil {
					return err
				}
				moves[i].To = saved
				continue
			}
			if err := tx.ReplaceOnRadios(ctx, m.From.ID, m.To.ID); err != nil {
				return fmt.Errorf("%s: %w", m.From.Name, err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return moves, left, nil
}

// History returns a shared channel's revisions, or a link's shared
// channel's, oldest first.
func (s *Service) History(ctx context.Context, ref string) (domain.Channel, []domain.Revision, error) {
	c, err := s.GetChannel(ctx, ref)
	if err != nil {
		return c, nil, err
	}
	revs, err := s.lib.ChannelRevisions(ctx, c.ID)
	return c, revs, err
}

// --- maintainer operations on the shared library ---

// errNotShared is a maintainer operation run on another library.
var errNotShared = domain.Invalidf("this changes the shared library; run it there (rflib shared ...)")

// UpdateShared saves a change to a shared channel, recording reason in its
// revision. Its tags must be curated ones.
func (s *Service) UpdateShared(ctx context.Context, c domain.Channel, reason string) (domain.Channel, []string, error) {
	if !s.info.Shared {
		return c, nil, errNotShared
	}
	if err := domain.ValidateSharedTags(domain.NormalizeTags(c.Tags)); err != nil {
		return c, nil, err
	}
	c, err := s.lib.UpdateChannel(store.WithChange(ctx, reason, ""), c)
	if err != nil {
		return c, nil, err
	}
	return c, domain.CheckBandPlan(c), nil
}

// StatusChange sets a channel's status. BackOn applies only to off-air
// channels, and ReplacedBy (an ID or prefix) only to retired ones.
type StatusChange struct {
	Status     domain.Status
	Note       string
	BackOn     *time.Time
	ReplacedBy string
}

// SetStatus changes one channel's status (see SetStatuses).
func (s *Service) SetStatus(ctx context.Context, ref string, ch StatusChange, reason string) (domain.Channel, error) {
	cs, err := s.SetStatuses(ctx, Selection{IDs: []string{ref}}, ch, reason)
	if err != nil {
		return domain.Channel{}, err
	}
	return cs[0], nil
}

// SetStatuses changes the selected channels' status in one transaction,
// recording reason, and returns them as saved. Channels are retired, never
// deleted, so they and their places on radios stay. In the
// shared library this is the maintainer's; in a private library it's for
// the library's own channels, since a shared channel's status is the
// shared list's. A replacement is one of the library's own channels or,
// from a private library, a shared one.
func (s *Service) SetStatuses(ctx context.Context, sel Selection, ch StatusChange, reason string) ([]domain.Channel, error) {
	if ch.BackOn != nil && ch.Status != domain.StatusOffAir {
		return nil, domain.Invalidf("an expected-back date is only for off-air channels")
	}
	if ch.ReplacedBy != "" && ch.Status != domain.StatusRetired {
		return nil, domain.Invalidf("a replacement is only for retired channels")
	}
	cs, err := s.Select(ctx, sel)
	if err != nil {
		return nil, err
	}
	if ch.ReplacedBy != "" {
		if ch.ReplacedBy, err = s.ChannelID(ctx, ch.ReplacedBy); err != nil {
			return nil, fmt.Errorf("replacement: %w", err)
		}
	}
	for _, c := range cs {
		if c.Link != nil {
			return nil, domain.Invalidf("%s is a shared channel, whose status is the shared list's; copy it to make it your own", c.Name)
		}
		if c.ID == ch.ReplacedBy {
			return nil, domain.Invalidf("%s can't replace itself", c.Name)
		}
	}
	ctx = store.WithChange(ctx, reason, "")
	err = s.lib.InTx(ctx, func(tx store.Library) error {
		for i := range cs {
			cs[i].Status, cs[i].StatusNote, cs[i].BackOn, cs[i].ReplacedBy = ch.Status, ch.Note, ch.BackOn, ch.ReplacedBy
			saved, err := tx.UpdateChannel(ctx, cs[i])
			if err != nil {
				return fmt.Errorf("%s: %w", cs[i].Name, err)
			}
			cs[i] = saved
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return cs, nil
}

// ImportShared writes a batch into the shared library, recording reason in
// each revision. Tags in the batch that aren't curated are dropped and
// returned; the tags opts adds to every channel must all be curated.
func (s *Service) ImportShared(ctx context.Context, b importer.Batch, opts importer.Options, reason string) (importer.Summary, []string, error) {
	if !s.info.Shared {
		return importer.Summary{}, nil, errNotShared
	}
	if err := domain.ValidateSharedTags(domain.NormalizeTags(opts.Tags)); err != nil {
		return importer.Summary{}, nil, err
	}
	dropped := curateTags(&b)
	ctx = store.WithChange(ctx, reason, "")
	// One transaction: organizations go first, for the channels to refer
	// to, and systems and plans last, since they refer to channels. A dry
	// run does it all and rolls back, so it checks the references too.
	var sum importer.Summary
	err := s.lib.InTx(ctx, func(tx store.Library) error {
		if err := restoreOrgs(ctx, tx, b.Shared.Orgs); err != nil {
			return err
		}
		write := opts
		write.DryRun = false
		var err error
		if sum, err = importer.Apply(ctx, tx, b, write); err != nil {
			return err
		}
		if err := restoreSystemsAndPlans(ctx, tx, b.Shared); err != nil {
			return err
		}
		if opts.DryRun {
			return errDryRun
		}
		return nil
	})
	if errors.Is(err, errDryRun) {
		err = nil
	}
	return sum, dropped, err
}

// errDryRun rolls back a dry run's transaction.
var errDryRun = errors.New("dry run")

// restoreOrgs creates a backup's organizations, or updates those already
// here, keeping their IDs.
func restoreOrgs(ctx context.Context, lib store.Library, orgs []domain.Organization) error {
	for _, o := range orgs {
		_, err := lib.GetOrg(ctx, o.ID)
		switch {
		case errors.Is(err, store.ErrNotFound):
			_, err = lib.CreateOrg(ctx, o)
		case err == nil:
			_, err = lib.UpdateOrg(ctx, o)
		}
		if err != nil {
			return fmt.Errorf("organization %s: %w", o.Name, err)
		}
	}
	return nil
}

// restoreSystemsAndPlans creates a backup's systems and plans, or updates
// those already here.
func restoreSystemsAndPlans(ctx context.Context, lib store.Library, r export.Records) error {
	for _, x := range r.Systems {
		_, err := lib.GetSystem(ctx, x.ID)
		switch {
		case errors.Is(err, store.ErrNotFound):
			_, err = lib.CreateSystem(ctx, x)
		case err == nil:
			_, err = lib.UpdateSystem(ctx, x)
		}
		if err != nil {
			return fmt.Errorf("system %s: %w", x.Name, err)
		}
	}
	for _, p := range r.Plans {
		_, err := lib.GetPlan(ctx, p.ID)
		switch {
		case errors.Is(err, store.ErrNotFound):
			_, err = lib.CreatePlan(ctx, p)
		case err == nil:
			_, err = lib.UpdatePlan(ctx, p)
		}
		if err != nil {
			return fmt.Errorf("plan %s: %w", p.Name, err)
		}
	}
	return nil
}

// sharedRecords returns the shared library's organizations, systems, and
// plans, for a backup.
func (s *Service) sharedRecords(ctx context.Context) (*export.Records, error) {
	var r export.Records
	var err error
	if r.Orgs, err = s.lib.ListOrgs(ctx); err != nil {
		return nil, err
	}
	if r.Systems, err = s.lib.ListSystems(ctx); err != nil {
		return nil, err
	}
	if r.Plans, err = s.lib.ListPlans(ctx); err != nil {
		return nil, err
	}
	return &r, nil
}

// curateTags keeps only curated tags on a batch's channels and returns the
// others it dropped.
func curateTags(b *importer.Batch) []string {
	var dropped []string
	for i := range b.Records {
		c := &b.Records[i].Channel
		var keep []string
		for _, t := range domain.NormalizeTags(c.Tags) {
			if domain.ValidateSharedTags([]string{t}) == nil {
				keep = append(keep, t)
			} else if !slices.Contains(dropped, t) {
				dropped = append(dropped, t)
			}
		}
		c.Tags = keep
	}
	slices.Sort(dropped)
	return dropped
}

// --- built-in lists ---

// SeedID is the shared channel ID a built-in list's row gets: the same in
// every database, so a list updates the channels it seeded.
func SeedID(list, name string) string {
	h := sha1.Sum([]byte("rflib-seed\x00" + list + "\x00" + name))
	return fmt.Sprintf("%x-%x-%x-%x-%x", h[0:4], h[4:6], h[6:8], h[8:10], h[10:16])
}
