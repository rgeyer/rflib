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

// The shared library holds channels shared with everyone. Other libraries
// link to them: a link shows the shared channel and follows its changes,
// and keeps the user's own tags, notes, skip, power, and short names.
// Everyday commands can't change the shared library; the operations here
// marked as maintainer operations do, and record a revision for each
// change.

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

// LinkedEdit refuses a change to a link's shared fields: those belong to
// the shared channel. old is the link as stored, c as it would be saved.
func LinkedEdit(old, c domain.Channel) error {
	if old.Link == nil || c.Link == nil || old.Link.SharedID != c.Link.SharedID {
		return nil
	}
	a, b := old, c
	a.Tags, a.Notes, b.Tags, b.Notes = nil, "", nil, ""
	if changes := domain.SharedChanges(a, b); len(changes) > 0 {
		field, _, _ := strings.Cut(changes[0], ":")
		return domain.Invalidf("%s is linked to a shared channel, so its %s can't change here; make it a private copy with rflib channel copy --id %s", old.Name, field, old.ID)
	}
	return nil
}

// localQuery refuses in:shared where only the library's own channels make
// sense, such as what goes on a radio.
func localQuery(n query.Node) error {
	if scope, _, err := query.SplitScope(n); err != nil || scope != "shared" {
		return err
	}
	return domain.Invalidf("in:shared finds shared channels to link; link them first (rflib channel link), then use them here")
}

// --- linking ---

// LinkResult is what linking did: the links made, and the shared channels
// skipped because the library already links to them.
// LinkIDs is the library's link to each selected shared channel, new or
// already there, in the selection's order, so they can go on a radio.
type LinkResult struct {
	Linked  []domain.Channel
	Already []domain.Channel
	LinkIDs []string
}

// LinkChannels links the selected shared channels into the library. The
// selection's IDs and query refer to the shared list. Own tags are added
// to every link.
func (s *Service) LinkChannels(ctx context.Context, sel Selection, tags []string) (LinkResult, error) {
	var res LinkResult
	if err := s.writable(); err != nil {
		return res, err
	}
	sh, err := s.shared(ctx)
	if err != nil {
		return res, err
	}
	targets, err := sh.Select(ctx, sel)
	if err != nil {
		return res, err
	}
	links, err := s.linksTo(ctx)
	if err != nil {
		return res, err
	}
	err = s.lib.InTx(ctx, func(tx store.Library) error {
		for _, t := range targets {
			if id, ok := links[t.ID]; ok {
				res.Already = append(res.Already, t)
				res.LinkIDs = append(res.LinkIDs, id)
				continue
			}
			c, err := tx.CreateChannel(ctx, domain.Channel{Tags: tags, Link: &domain.Link{SharedID: t.ID, SeenRevision: t.Revision}})
			if err != nil {
				return fmt.Errorf("%s: %w", t.Name, err)
			}
			links[t.ID] = c.ID
			res.Linked = append(res.Linked, c)
			res.LinkIDs = append(res.LinkIDs, c.ID)
		}
		return nil
	})
	return res, err
}


// linksTo maps each shared channel the library links to to its link's ID.
func (s *Service) linksTo(ctx context.Context) (map[string]string, error) {
	cs, err := s.lib.ListChannels(ctx)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, c := range cs {
		if c.Link != nil {
			out[c.Link.SharedID] = c.ID
		}
	}
	return out, nil
}

// Conversion is a private channel that matches a shared one, and the link
// it becomes.
type Conversion struct {
	Private, Shared, Link domain.Channel
}

// ConvertToLinks turns private channels into links to the shared channels
// they match: the same frequencies, tones, and tone squelch
// (domain.ProgrammedKey), and the same service unless the private channel
// has none, since FRS and GMRS share frequencies. Of several matches, one
// with the same name wins, then an active one. The channel keeps its ID, so
// radios keep it; its tags and notes stay its own, less what the shared
// channel already has, and a name that differs from the shared one is kept
// as a short name. only, when not nil, limits the shared channels
// considered, and ids, when not nil, limits the private channels. With
// dryRun nothing changes.
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
	var out []Conversion
	for _, c := range mine {
		if c.Link != nil || (ids != nil && !slices.Contains(ids, c.ID)) {
			continue
		}
		target, ok := bestMatch(c, byKey[domain.ProgrammedKey(c)])
		if !ok {
			continue
		}
		link := c
		link.Link = &domain.Link{SharedID: target.ID, SeenRevision: target.Revision}
		link.Tags = slices.DeleteFunc(slices.Clone(c.Tags), func(t string) bool { return slices.Contains(target.Tags, t) })
		if strings.TrimSpace(c.Notes) == strings.TrimSpace(target.Notes) {
			link.Notes = ""
		}
		if c.Name != target.Name && !slices.Contains(c.ShortNames, c.Name) {
			link.ShortNames = append(slices.Clone(c.ShortNames), c.Name)
		}
		out = append(out, Conversion{Private: c, Shared: target, Link: domain.Linked(link, target)})
	}
	if dryRun || len(out) == 0 {
		return out, nil
	}
	err = s.lib.InTx(ctx, func(tx store.Library) error {
		for i, cv := range out {
			saved, err := tx.UpdateChannel(ctx, cv.Link)
			if err != nil {
				return fmt.Errorf("%s: %w", cv.Private.Name, err)
			}
			out[i].Link = saved
		}
		return nil
	})
	return out, err
}

// bestMatch picks the shared channel a private one should link to from
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

// CopyChannels turns the selected links into private channels showing what
// they showed, with the shared tags and notes made their own. They stop
// following the shared channel and remember it in CopiedFrom. Private
// channels in the selection are left alone.
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
			c.Tags, c.Notes, c.CopiedFrom = c.AllTags(), c.AllNotes(), c.Link.SharedID
			c.Link, c.Status, c.StatusNote, c.BackOn, c.ReplacedBy = nil, domain.StatusActive, "", nil, ""
			saved, err := tx.UpdateChannel(ctx, c)
			if err != nil {
				return fmt.Errorf("%s: %w", c.Name, err)
			}
			out = append(out, saved)
		}
		return nil
	})
	return out, err
}

// --- changes ---

// Change is a link whose shared channel changed since the user last
// acknowledged it: what changed, why, the radios it's on, and, for a
// retired channel, its replacement.
type Change struct {
	Channel     domain.Channel
	Changes     []string
	Reasons     []string
	Radios      []string
	Replacement *domain.Channel
}

// Changes lists the library's links whose shared channel changed.
func (s *Service) Changes(ctx context.Context) ([]Change, error) {
	cs, err := s.lib.ListChannels(ctx)
	if err != nil {
		return nil, err
	}
	onRadios, err := s.radioNames(ctx)
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

// radioNames maps each channel to the radios it's on.
func (s *Service) radioNames(ctx context.Context) (map[string][]string, error) {
	rs, err := s.lib.ListRadios(ctx)
	if err != nil {
		return nil, err
	}
	out := map[string][]string{}
	for _, r := range rs {
		on, err := s.lib.RadioChannelIDs(ctx, r.ID)
		if err != nil {
			return nil, err
		}
		for id := range on {
			out[id] = append(out[id], r.Name)
		}
	}
	return out, nil
}

// Acknowledge marks the selected links' shared changes as seen, and
// returns the links that had changes.
func (s *Service) Acknowledge(ctx context.Context, sel Selection) ([]domain.Channel, error) {
	return s.eachChangedLink(ctx, sel, func(_ context.Context, c *domain.Channel) (bool, error) {
		c.Link.SeenRevision = c.Revision
		return true, nil
	})
}

// Relink moves the selected links to a retired shared channel over to its
// replacement, and returns them.
func (s *Service) Relink(ctx context.Context, sel Selection) ([]domain.Channel, error) {
	sh, err := s.sharedLibrary(ctx)
	if err != nil {
		return nil, err
	}
	// Replacements are looked up before the library's transaction starts:
	// the shared library can't be read inside it.
	cs, err := s.Select(ctx, sel)
	if err != nil {
		return nil, err
	}
	next := map[string]domain.Channel{}
	for _, c := range cs {
		if c.Changed() && c.Status.OrActive() == domain.StatusRetired && c.ReplacedBy != "" {
			r, err := sh.GetChannel(ctx, c.ReplacedBy)
			if err != nil {
				return nil, fmt.Errorf("%s: replacement: %w", c.Name, err)
			}
			next[c.ID] = r
		}
	}
	return s.eachChangedLink(ctx, sel, func(_ context.Context, c *domain.Channel) (bool, error) {
		r, ok := next[c.ID]
		if ok {
			c.Link = &domain.Link{SharedID: r.ID, SeenRevision: r.Revision}
		}
		return ok, nil
	})
}

// eachChangedLink saves fn's change to each selected link whose shared
// channel changed, in one transaction. fn runs inside it, so it must not
// reach the shared library.
func (s *Service) eachChangedLink(ctx context.Context, sel Selection, fn func(context.Context, *domain.Channel) (bool, error)) ([]domain.Channel, error) {
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
			if !c.Changed() {
				continue
			}
			ok, err := fn(ctx, &c)
			if err != nil {
				return err
			}
			if !ok {
				continue
			}
			saved, err := tx.UpdateChannel(ctx, c)
			if err != nil {
				return fmt.Errorf("%s: %w", c.Name, err)
			}
			out = append(out, saved)
		}
		return nil
	})
	return out, err
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

// StatusChange sets a shared channel's status. BackOn applies only to
// off-air channels, and ReplacedBy (an ID or prefix) only to retired ones.
type StatusChange struct {
	Status     domain.Status
	Note       string
	BackOn     *time.Time
	ReplacedBy string
}

// SetStatus changes a shared channel's status, recording reason.
func (s *Service) SetStatus(ctx context.Context, ref string, ch StatusChange, reason string) (domain.Channel, error) {
	if !s.info.Shared {
		return domain.Channel{}, errNotShared
	}
	c, err := s.GetChannel(ctx, ref)
	if err != nil {
		return c, err
	}
	if ch.BackOn != nil && ch.Status != domain.StatusOffAir {
		return c, domain.Invalidf("an expected-back date is only for off-air channels")
	}
	if ch.ReplacedBy != "" {
		if ch.Status != domain.StatusRetired {
			return c, domain.Invalidf("a replacement is only for retired channels")
		}
		if ch.ReplacedBy, err = s.ChannelID(ctx, ch.ReplacedBy); err != nil {
			return c, fmt.Errorf("replacement: %w", err)
		}
		if ch.ReplacedBy == c.ID {
			return c, domain.Invalidf("a channel can't replace itself")
		}
	}
	c.Status, c.StatusNote, c.BackOn, c.ReplacedBy = ch.Status, ch.Note, ch.BackOn, ch.ReplacedBy
	return s.lib.UpdateChannel(store.WithChange(ctx, reason, ""), c)
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

