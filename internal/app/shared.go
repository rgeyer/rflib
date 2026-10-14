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
	"github.com/ryan/rflib/internal/importer"
	"github.com/ryan/rflib/internal/query"
	"github.com/ryan/rflib/internal/seed"
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
		return fmt.Errorf("%s is linked to a shared channel, so its %s can't change here; make it a private copy with rflib channel copy --id %s", old.Name, field, old.ID)
	}
	return nil
}

// localQuery refuses in:shared where only the library's own channels make
// sense, such as what goes on a radio.
func localQuery(n query.Node) error {
	if scope, _, err := query.SplitScope(n); err != nil || scope != "shared" {
		return err
	}
	return errors.New("in:shared finds shared channels to link; link them first (rflib channel link), then use them here")
}

// --- linking ---

// LinkResult is what linking did: the links made, and the shared channels
// skipped because the library already links to them.
type LinkResult struct {
	Linked  []domain.Channel
	Already []domain.Channel
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
	linked, err := s.linkedTo(ctx)
	if err != nil {
		return res, err
	}
	err = s.lib.InTx(ctx, func(tx store.Library) error {
		for _, t := range targets {
			if linked[t.ID] {
				res.Already = append(res.Already, t)
				continue
			}
			c, err := tx.CreateChannel(ctx, domain.Channel{Tags: tags, Link: &domain.Link{SharedID: t.ID, SeenRevision: t.Revision}})
			if err != nil {
				return fmt.Errorf("%s: %w", t.Name, err)
			}
			linked[t.ID] = true
			res.Linked = append(res.Linked, c)
		}
		return nil
	})
	return res, err
}

// linkedTo returns the shared channels the library links to.
func (s *Service) linkedTo(ctx context.Context) (map[string]bool, error) {
	cs, err := s.lib.ListChannels(ctx)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, c := range cs {
		if c.Link != nil {
			out[c.Link.SharedID] = true
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
// considered. With dryRun nothing changes.
func (s *Service) ConvertToLinks(ctx context.Context, only map[string]bool, dryRun bool) ([]Conversion, error) {
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
	return s.convertTo(ctx, shared, only, dryRun)
}

// convertTo is ConvertToLinks against the given shared channels.
func (s *Service) convertTo(ctx context.Context, shared []domain.Channel, only map[string]bool, dryRun bool) ([]Conversion, error) {
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
		if c.Link != nil {
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
var errNotShared = errors.New("this changes the shared library; run it there (rflib shared ...)")

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
		return c, fmt.Errorf("an expected-back date is only for off-air channels")
	}
	if ch.ReplacedBy != "" {
		if ch.Status != domain.StatusRetired {
			return c, fmt.Errorf("a replacement is only for retired channels")
		}
		if ch.ReplacedBy, err = s.ChannelID(ctx, ch.ReplacedBy); err != nil {
			return c, fmt.Errorf("replacement: %w", err)
		}
		if ch.ReplacedBy == c.ID {
			return c, fmt.Errorf("a channel can't replace itself")
		}
	}
	c.Status, c.StatusNote, c.BackOn, c.ReplacedBy = ch.Status, ch.Note, ch.BackOn, ch.ReplacedBy
	return s.lib.UpdateChannel(store.WithChange(ctx, reason, ""), c)
}

// ImportShared writes a batch into the shared library, recording reason in
// each revision. Tags that aren't curated are dropped and returned.
func (s *Service) ImportShared(ctx context.Context, b importer.Batch, opts importer.Options, reason string) (importer.Summary, []string, error) {
	if !s.info.Shared {
		return importer.Summary{}, nil, errNotShared
	}
	dropped := curateTags(&b)
	sum, err := importer.Apply(store.WithChange(ctx, reason, ""), s.lib, b, opts)
	return sum, dropped, err
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

// SeedResult is what installing a built-in list did: shared channels added
// or updated from the list, private channels converted to links, links
// made, and channels already linked.
type SeedResult struct {
	SharedAdded, SharedUpdated int
	Converted                  []Conversion
	Linked, Already            []domain.Channel
}

// seedID is a built-in channel's ID in the shared library, the same in
// every database so a list can be installed again, and pulled later.
func seedID(list, name string) string {
	h := sha1.Sum([]byte("rflib-seed\x00" + list + "\x00" + name))
	return fmt.Sprintf("%x-%x-%x-%x-%x", h[0:4], h[4:6], h[6:8], h[8:10], h[10:16])
}

// InstallSeed puts a built-in list's channels in the shared library (or
// updates them there), then gives the library a link to each: private
// channels that match one become links (see ConvertToLinks), and the rest
// are linked new. Tags not in the curated set are left off the shared
// channels. Own tags are added to new links. With dryRun nothing changes.
func (s *Service) InstallSeed(ctx context.Context, l seed.List, tags []string, dryRun bool) (SeedResult, error) {
	var res SeedResult
	if err := s.writable(); err != nil {
		return res, err
	}
	b, err := l.Batch()
	if err != nil {
		return res, err
	}
	curateTags(&b)
	sh, err := s.sharedLibrary(ctx)
	if err != nil {
		return res, err
	}
	ids := map[string]bool{}
	var want []domain.Channel
	for _, r := range b.Records {
		c := r.Channel
		c.ID = seedID(l.ID, c.Name)
		ids[c.ID] = true
		want = append(want, c)
	}
	if !dryRun {
		ctx := store.WithChange(ctx, "built-in list "+l.ID, "")
		err = sh.InTx(ctx, func(tx store.Library) error {
			for _, c := range want {
				old, err := tx.GetChannel(ctx, c.ID)
				switch {
				case errors.Is(err, store.ErrNotFound):
					if _, err := tx.CreateChannel(ctx, c); err != nil {
						return fmt.Errorf("%s: %w", c.Name, err)
					}
					res.SharedAdded++
				case err != nil:
					return err
				default:
					// Keep what the list doesn't say, such as a status.
					c.Status, c.StatusNote, c.BackOn, c.ReplacedBy, c.CreatedAt = old.Status, old.StatusNote, old.BackOn, old.ReplacedBy, old.CreatedAt
					saved, err := tx.UpdateChannel(ctx, c)
					if err != nil {
						return fmt.Errorf("%s: %w", c.Name, err)
					}
					if saved.Revision != old.Revision {
						res.SharedUpdated++
					}
				}
			}
			return nil
		})
		if err != nil {
			return res, err
		}
	}
	// A dry run matches against the list as the install would leave it.
	target := want
	if !dryRun {
		if target, err = sh.ListChannels(ctx); err != nil {
			return res, err
		}
	}
	if res.Converted, err = s.convertTo(ctx, target, ids, dryRun); err != nil {
		return res, err
	}
	linked, err := s.linkedTo(ctx)
	if err != nil {
		return res, err
	}
	for _, cv := range res.Converted {
		linked[cv.Shared.ID] = true
	}
	var toLink []string
	for _, c := range want {
		if linked[c.ID] {
			res.Already = append(res.Already, c)
		} else {
			toLink = append(toLink, c.ID)
		}
	}
	res.Already = slices.DeleteFunc(res.Already, func(c domain.Channel) bool {
		return slices.ContainsFunc(res.Converted, func(cv Conversion) bool { return cv.Shared.ID == c.ID })
	})
	if dryRun || len(toLink) == 0 {
		for _, id := range toLink {
			i := slices.IndexFunc(want, func(c domain.Channel) bool { return c.ID == id })
			res.Linked = append(res.Linked, want[i])
		}
		return res, nil
	}
	lr, err := s.LinkChannels(ctx, Selection{IDs: toLink}, tags)
	res.Linked = lr.Linked
	return res, err
}
