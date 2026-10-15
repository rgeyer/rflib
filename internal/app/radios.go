package app

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/ryan/rflib/internal/domain"
	"github.com/ryan/rflib/internal/model"
	"github.com/ryan/rflib/internal/program"
	"github.com/ryan/rflib/internal/query"
	"github.com/ryan/rflib/internal/store"
)

// CreateRadio adds a radio of a known model.
func (s *Service) CreateRadio(ctx context.Context, r domain.Radio) (domain.Radio, error) {
	if err := s.writable(); err != nil {
		return r, err
	}
	if _, err := model.Find(r.Model); err != nil {
		return r, err
	}
	return s.lib.CreateRadio(ctx, r)
}

func (s *Service) GetRadio(ctx context.Context, idOrName string) (domain.Radio, error) {
	return s.lib.GetRadio(ctx, idOrName)
}

func (s *Service) ListRadios(ctx context.Context) ([]domain.Radio, error) {
	return s.lib.ListRadios(ctx)
}

func (s *Service) UpdateRadio(ctx context.Context, r domain.Radio) (domain.Radio, error) {
	if _, err := model.Find(r.Model); err != nil {
		return r, err
	}
	return s.lib.UpdateRadio(ctx, r)
}

// DeleteRadio deletes a radio by ID or name; its channels stay in the
// library.
func (s *Service) DeleteRadio(ctx context.Context, idOrName string) error {
	r, err := s.lib.GetRadio(ctx, idOrName)
	if err != nil {
		return err
	}
	return s.lib.DeleteRadio(ctx, r.ID)
}

// RadioEntries returns a radio's channels in memory order.
func (s *Service) RadioEntries(ctx context.Context, idOrName string) (domain.Radio, []domain.RadioEntry, error) {
	r, err := s.lib.GetRadio(ctx, idOrName)
	if err != nil {
		return r, nil, err
	}
	es, err := s.lib.RadioEntries(ctx, r.ID)
	return r, es, err
}

// RadioModel returns a radio and its model.
func (s *Service) RadioModel(ctx context.Context, idOrName string) (domain.Radio, model.Model, error) {
	r, err := s.lib.GetRadio(ctx, idOrName)
	if err != nil {
		return r, model.Model{}, err
	}
	m, err := model.Find(r.Model)
	return r, m, err
}

// Layout lays out a radio's memories (see program.Build). The entries are in
// the same order as the layout's memories.
func (s *Service) Layout(ctx context.Context, idOrName string) (program.Layout, []domain.RadioEntry, error) {
	r, m, err := s.RadioModel(ctx, idOrName)
	if err != nil {
		return program.Layout{}, nil, err
	}
	es, err := s.lib.RadioEntries(ctx, r.ID)
	if err != nil {
		return program.Layout{}, nil, err
	}
	p, err := program.Build(r, m, es)
	return p, es, err
}

// relayout reports an error when a change leaves a radio unable to export,
// such as a section start behind the channels before it.
func (s *Service) relayout(ctx context.Context, r domain.Radio) error {
	if _, _, err := s.Layout(ctx, r.ID); err != nil {
		return fmt.Errorf("saved, but %s no longer exports: %w", r.Name, err)
	}
	return nil
}

// AddRequest is what to add to a radio: channels by ID, then the matches
// of Query in Sort order.
// AddRequest is what to add to a radio: channels by ID (or ID prefix), in
// the order given, or a query's matches in Sort order. With both, a
// channel must be in both.
type AddRequest struct {
	IDs   []string
	Query query.Node
	Sort  string
	// AnyBand adds channels outside the radio's bands, too, and AnyMode
	// channels in modes it doesn't have.
	AnyBand, AnyMode bool
}

// AddResult is what an add or a source's sync did. Add lists what was (or
// would be) added, Skipped what is outside the radio's bands, and
// WrongMode what is in a mode the radio doesn't have.
type AddResult struct {
	Add       []domain.Channel
	Added     int
	Skipped   []domain.Channel
	WrongMode []domain.Channel
}

// fit sorts channels into those the model can receive, those outside its
// bands, and those in its bands but in a mode it lacks. A transmit
// frequency outside the bands only warns (see program.Build), since the
// radio can still listen.
func (r *AddResult) fit(m model.Model, cs []domain.Channel, anyBand, anyMode bool) {
	for _, c := range cs {
		switch {
		case !anyBand && !m.Tunes(c.ReceiveHz):
			r.Skipped = append(r.Skipped, c)
		case !anyMode && !m.HasMode(c.Mode):
			r.WrongMode = append(r.WrongMode, c)
		default:
			r.Add = append(r.Add, c)
		}
	}
}

// AddChannels appends channels to the end of a radio's list. Channels
// already on it are left where they are. Channels outside its bands are
// skipped unless req.AnyBand, and channels in modes it lacks unless
// req.AnyMode.
func (s *Service) AddChannels(ctx context.Context, radio string, req AddRequest) (domain.Radio, AddResult, error) {
	r, m, err := s.RadioModel(ctx, radio)
	if err != nil {
		return r, AddResult{}, err
	}
	var cs []domain.Channel
	if req.Query == nil {
		ids, err := s.ChannelIDs(ctx, req.IDs)
		if err != nil {
			return r, AddResult{}, err
		}
		for _, id := range ids {
			c, err := s.lib.GetChannel(ctx, id)
			if err != nil {
				return r, AddResult{}, err
			}
			cs = append(cs, c)
		}
	} else {
		// A query picks from the library's channels and the shared list.
		if cs, err = s.FindForRadios(ctx, req.Query); err != nil {
			return r, AddResult{}, err
		}
		if len(req.IDs) > 0 {
			ids, err := s.ChannelIDs(ctx, req.IDs)
			if err != nil {
				return r, AddResult{}, err
			}
			cs = slices.DeleteFunc(cs, func(c domain.Channel) bool { return !slices.Contains(ids, c.ID) })
		}
		if err := domain.SortChannels(cs, orDefault(req.Sort, "name")); err != nil {
			return r, AddResult{}, err
		}
	}
	var res AddResult
	res.fit(m, cs, req.AnyBand, req.AnyMode)
	ids := make([]string, len(res.Add))
	for i, c := range res.Add {
		ids[i] = c.ID
	}
	res.Added, err = s.lib.AddRadioChannels(ctx, r.ID, ids)
	return r, res, err
}

func orDefault(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

// RemoveChannels takes channels off a radio and returns how many.
// OnRadio returns the selected channels that are on a radio, in name
// order.
func (s *Service) OnRadio(ctx context.Context, radio string, sel Selection) (domain.Radio, []domain.Channel, error) {
	r, err := s.lib.GetRadio(ctx, radio)
	if err != nil {
		return r, nil, err
	}
	on, err := s.lib.RadioChannelIDs(ctx, r.ID)
	if err != nil {
		return r, nil, err
	}
	cs, err := s.selectIn(ctx, sel, on)
	return r, cs, err
}

// RemoveChannels takes the selected channels off a radio; they stay in the
// library. It returns the channels taken off.
func (s *Service) RemoveChannels(ctx context.Context, radio string, sel Selection) (domain.Radio, []domain.Channel, error) {
	r, cs, err := s.OnRadio(ctx, radio, sel)
	if err != nil || len(cs) == 0 {
		return r, cs, err
	}
	ids := make([]string, len(cs))
	for i, c := range cs {
		ids[i] = c.ID
	}
	_, err = s.lib.RemoveRadioChannels(ctx, r.ID, ids)
	return r, cs, err
}

// Move is where to move a channel in a radio's list: before or after
// another channel, to a position (from 1), or to the top or bottom.
type Move struct {
	Before, After string
	To            int
	Top, Bottom   bool
}

// MoveChannel moves a channel within a radio's list.
func (s *Service) MoveChannel(ctx context.Context, radio, channelID string, mv Move) error {
	r, es, err := s.RadioEntries(ctx, radio)
	if err != nil {
		return err
	}
	index := func(ref string) (int, error) {
		id, err := s.ChannelID(ctx, ref)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return 0, err
		}
		for _, e := range es {
			if e.Channel.ID == id {
				return e.Position, nil
			}
		}
		return 0, fmt.Errorf("channel %q is not on %s: %w", ref, r.Name, store.ErrNotFound)
	}
	from, err := index(channelID)
	if err != nil {
		return err
	}
	channelID = es[from].Channel.ID
	var to int
	switch {
	case mv.Top:
		to = 0
	case mv.Bottom:
		to = len(es)
	case mv.To != 0:
		if mv.To < 1 {
			return domain.Invalidf("positions count from 1")
		}
		to = mv.To - 1
	case mv.Before != "" || mv.After != "":
		if to, err = index(mv.Before + mv.After); err != nil {
			return err
		}
		// Positions are counted after the moving channel is taken out.
		if to > from {
			to--
		}
		if mv.After != "" {
			to++
		}
	default:
		return domain.Invalidf("say where to move the channel")
	}
	return s.lib.MoveRadioChannel(ctx, r.ID, channelID, to)
}

// RadioEntry returns a radio's entry for one of its channels.
func (s *Service) RadioEntry(ctx context.Context, radio, channelID string) (domain.Radio, domain.RadioEntry, error) {
	r, es, err := s.RadioEntries(ctx, radio)
	if err != nil {
		return r, domain.RadioEntry{}, err
	}
	if id, err := s.ChannelID(ctx, channelID); err == nil {
		channelID = id
	} else if !errors.Is(err, store.ErrNotFound) {
		return r, domain.RadioEntry{}, err
	}
	for _, e := range es {
		if e.Channel.ID == channelID {
			return r, e, nil
		}
	}
	return r, domain.RadioEntry{}, fmt.Errorf("channel %q is not on %s: %w", channelID, r.Name, store.ErrNotFound)
}

// SetEntry saves a radio's overrides for one of its channels, and reports
// an error if the radio no longer lays out, such as a section start behind
// the channels before it. The overrides are saved either way.
func (s *Service) SetEntry(ctx context.Context, radio string, e domain.RadioEntry) error {
	r, err := s.lib.GetRadio(ctx, radio)
	if err != nil {
		return err
	}
	if err := s.lib.SetRadioEntry(ctx, r.ID, e); err != nil {
		return err
	}
	return s.relayout(ctx, r)
}

// EntryEdit is a change to a radio's overrides for many channels at once.
// Unset fields are left alone; a set Skip of nil removes the override.
type EntryEdit struct {
	SetPower bool
	Power    domain.PowerIntent
	SetSkip  bool
	Skip     *bool
}

// SetEntries applies an edit to the radio's overrides for each selected
// channel on it, and returns the channels changed. Like SetEntry, it
// reports an error if the radio no longer lays out; the overrides are saved
// either way.
func (s *Service) SetEntries(ctx context.Context, radio string, sel Selection, edit EntryEdit) ([]domain.Channel, error) {
	r, cs, err := s.OnRadio(ctx, radio, sel)
	if err != nil {
		return nil, err
	}
	es, err := s.lib.RadioEntries(ctx, r.ID)
	if err != nil {
		return nil, err
	}
	err = s.lib.InTx(ctx, func(tx store.Library) error {
		for _, c := range cs {
			i := slices.IndexFunc(es, func(e domain.RadioEntry) bool { return e.Channel.ID == c.ID })
			e := es[i]
			if edit.SetPower {
				e.Power = edit.Power
			}
			if edit.SetSkip {
				e.Skip = edit.Skip
			}
			if err := tx.SetRadioEntry(ctx, r.ID, e); err != nil {
				return fmt.Errorf("%s: %w", c.Name, err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return cs, s.relayout(ctx, r)
}

// --- sources ---

// Sources returns a radio's sources and its entries, so callers can count
// each source's channels.
func (s *Service) Sources(ctx context.Context, radio string) (domain.Radio, []domain.RadioSource, []domain.RadioEntry, error) {
	r, es, err := s.RadioEntries(ctx, radio)
	if err != nil {
		return r, nil, nil, err
	}
	sources, err := s.lib.RadioSources(ctx, r.ID)
	return r, sources, es, err
}

// Source returns a radio's source by number, from 1.
func (s *Service) Source(ctx context.Context, radio string, number int) (domain.Radio, domain.RadioSource, error) {
	r, err := s.lib.GetRadio(ctx, radio)
	if err != nil {
		return r, domain.RadioSource{}, err
	}
	sources, err := s.lib.RadioSources(ctx, r.ID)
	if err != nil {
		return r, domain.RadioSource{}, err
	}
	if number < 1 || number > len(sources) {
		return r, domain.RadioSource{}, domain.Invalidf("%s has sources 1-%d; see rflib radio sources list %s", r.Name, len(sources), r.Name)
	}
	return r, sources[number-1], nil
}

// AddSource appends a source to a radio after checking its query. A
// plan source's PlanID may be the plan's name.
func (s *Service) AddSource(ctx context.Context, radio string, src domain.RadioSource) (domain.Radio, domain.RadioSource, error) {
	r, err := s.lib.GetRadio(ctx, radio)
	if err != nil {
		return r, src, err
	}
	if err := s.checkSource(ctx, &src); err != nil {
		return r, src, err
	}
	src.RadioID, src.Sort = r.ID, orDefault(src.Sort, "name")
	src, err = s.lib.AddRadioSource(ctx, src)
	return r, src, err
}

// checkSource checks a new source's query, or resolves its plan, whose
// PlanID may be the plan's name.
func (s *Service) checkSource(ctx context.Context, src *domain.RadioSource) error {
	if src.PlanID == "" {
		_, err := s.CheckQuery(ctx, src.Query, nil)
		return err
	}
	if src.Query != "" {
		return domain.Invalidf("a source is a query or a plan, not both")
	}
	p, err := s.GetPlan(ctx, src.PlanID)
	if err != nil {
		return err
	}
	src.PlanID, src.PlanName = p.ID, p.Name
	return nil
}

// UpdateSource saves a changed source after checking its query, and
// reports an error if the radio no longer lays out.
func (s *Service) UpdateSource(ctx context.Context, src domain.RadioSource) error {
	r, err := s.lib.GetRadio(ctx, src.RadioID)
	if err != nil {
		return err
	}
	if src.PlanID == "" {
		if _, err := s.CheckQuery(ctx, src.Query, nil); err != nil {
			return err
		}
	}
	if err := s.lib.UpdateRadioSource(ctx, src); err != nil {
		return err
	}
	return s.relayout(ctx, r)
}

// MoveSource moves a radio's source number from to number to (both from
// 1).
func (s *Service) MoveSource(ctx context.Context, radio string, from, to int) error {
	if to < 1 {
		return domain.Invalidf("source numbers count from 1")
	}
	_, src, err := s.Source(ctx, radio, from)
	if err != nil {
		return err
	}
	return s.lib.MoveRadioSource(ctx, src, to-1)
}

// RemoveSource deletes a radio's source by number. Its channels stay unless
// withChannels, or it's a plan, whose channels always go with it; it
// returns how many were taken off.
func (s *Service) RemoveSource(ctx context.Context, radio string, number int, withChannels bool) (int, error) {
	_, src, err := s.Source(ctx, radio, number)
	if err != nil {
		return 0, err
	}
	return s.lib.RemoveRadioSource(ctx, src, withChannels || src.PlanID != "")
}

// SyncResult is what a radio sync did, source by source. LayoutErr is set
// when the radio no longer lays out afterwards, such as a source running past
// the next section start.
type SyncResult struct {
	Radio     domain.Radio
	Sources   []SourceSync
	LayoutErr error
}

type SourceSync struct {
	Source domain.RadioSource
	AddResult
	// Resorted is how many of the source's channels already on the radio
	// changed places when it was re-sorted, or for a plan, put in the
	// plan's order.
	Resorted int
	// For a plan: Removed lists the channels taken off because the plan
	// dropped them, and Note says why nothing changed, such as an archived
	// plan.
	Removed []domain.Channel
	Note    string
}

// Added is how many channels the sync added in all.
func (r SyncResult) Added() int {
	n := 0
	for _, s := range r.Sources {
		n += s.Added
	}
	return n
}

// SyncOptions are a sync's choices. With DryRun nothing changes. AnyBand
// adds channels outside the radio's bands, and AnyMode channels in modes it
// doesn't have. Resort re-sorts every source, not only those set to
// KeepSorted.
type SyncOptions struct {
	DryRun, AnyBand, AnyMode, Resort bool
}

// Sync adds each source's new matches to a radio (see AddSourceChannels).
// An earlier source claims a channel first; channels outside the radio's
// bands or in modes it lacks are skipped, and stay unclaimed, unless
// AnyBand or AnyMode. With DryRun
// nothing changes, Add lists what would be added, and Resorted counts the
// channels already on the radio that would move.
//
// A re-sorted source's channels are put in its sort order within the
// places in the list they hold; channels added or moved by hand keep
// theirs, and section starts stay where they are.
func (s *Service) Sync(ctx context.Context, radio string, opts SyncOptions) (SyncResult, error) {
	r, m, err := s.RadioModel(ctx, radio)
	res := SyncResult{Radio: r}
	if err != nil {
		return res, err
	}
	sources, err := s.lib.RadioSources(ctx, r.ID)
	if err != nil {
		return res, err
	}
	if len(sources) == 0 {
		return res, domain.Invalidf("%s has no sources; add one with: rflib radio sources add %s QUERY", r.Name, r.Name)
	}
	if res.Sources, err = s.syncSources(ctx, r, m, sources, opts); err != nil {
		return res, err
	}
	if err := s.resort(ctx, r, &res, opts); err != nil {
		return res, err
	}
	if !opts.DryRun {
		if _, _, err := s.Layout(ctx, r.ID); err != nil {
			res.LayoutErr = err
		}
	}
	return res, nil
}

// PreviewSource reports what a new source would add to a radio, after its
// existing sources claim their channels, without adding the source.
func (s *Service) PreviewSource(ctx context.Context, radio string, src domain.RadioSource, opts SyncOptions) (SourceSync, error) {
	r, m, err := s.RadioModel(ctx, radio)
	if err != nil {
		return SourceSync{}, err
	}
	if err := s.checkSource(ctx, &src); err != nil {
		return SourceSync{}, err
	}
	sources, err := s.lib.RadioSources(ctx, r.ID)
	if err != nil {
		return SourceSync{}, err
	}
	src.RadioID, src.Position, src.Sort = r.ID, len(sources), orDefault(src.Sort, "name")
	opts.DryRun = true
	synced, err := s.syncSources(ctx, r, m, append(sources, src), opts)
	if err != nil {
		return SourceSync{}, err
	}
	return synced[len(synced)-1], nil
}

// syncSources works out each source's additions in order (see Sync), and
// adds them unless opts.DryRun.
func (s *Service) syncSources(ctx context.Context, r domain.Radio, m model.Model, sources []domain.RadioSource, opts SyncOptions) ([]SourceSync, error) {
	dryRun, anyBand := opts.DryRun, opts.AnyBand
	var out []SourceSync
	claimed, err := s.lib.RadioChannelIDs(ctx, r.ID)
	if err != nil {
		return nil, err
	}
	// Plans claim their channels first: each is a curated list, so its
	// channels belong in its part of the radio whatever else matches them.
	plans := map[string]SourceSync{}
	for _, src := range sources {
		if src.PlanID != "" {
			ss, err := s.syncPlan(ctx, r, m, src, claimed, opts)
			if err != nil {
				return nil, fmt.Errorf("source %d: %w", src.Position+1, err)
			}
			plans[src.ID] = ss
		}
	}
	for _, src := range sources {
		if src.PlanID != "" {
			out = append(out, plans[src.ID])
			continue
		}
		n, err := query.Parse(src.Query)
		if err != nil {
			return nil, fmt.Errorf("source %d: %w", src.Position+1, err)
		}
		cs, err := s.FindForRadios(ctx, n)
		if err != nil {
			return nil, fmt.Errorf("source %d: %w", src.Position+1, err)
		}
		if err := domain.SortChannels(cs, src.Sort); err != nil {
			return nil, err
		}
		var unclaimed []domain.Channel
		for _, c := range cs {
			if !claimed[c.ID] {
				unclaimed = append(unclaimed, c)
			}
		}
		ss := SourceSync{Source: src}
		ss.fit(m, unclaimed, anyBand, opts.AnyMode)
		ids := make([]string, len(ss.Add))
		for i, c := range ss.Add {
			claimed[c.ID] = true
			ids[i] = c.ID
		}
		if !dryRun && len(ids) > 0 {
			if ss.Added, err = s.lib.AddSourceChannels(ctx, src, ids); err != nil {
				return nil, err
			}
		}
		out = append(out, ss)
	}
	return out, nil
}

// resort re-sorts the sources that ask for it, or all of them with
// opts.Resort, in one reorder of the radio's list.
func (s *Service) resort(ctx context.Context, r domain.Radio, res *SyncResult, opts SyncOptions) error {
	es, err := s.lib.RadioEntries(ctx, r.ID)
	if err != nil {
		return err
	}
	order := make([]string, len(es))
	for i, e := range es {
		order[i] = e.Channel.ID
	}
	moved := 0
	for i := range res.Sources {
		ss := &res.Sources[i]
		if ss.Source.PlanID != "" || !opts.Resort && !ss.Source.KeepSorted {
			continue
		}
		var slots []int
		var cs []domain.Channel
		for j, e := range es {
			if e.SourceID == ss.Source.ID {
				slots = append(slots, j)
				cs = append(cs, e.Channel)
			}
		}
		if err := domain.SortChannels(cs, ss.Source.Sort); err != nil {
			return err
		}
		for k, j := range slots {
			if order[j] != cs[k].ID {
				order[j] = cs[k].ID
				ss.Resorted++
			}
		}
		moved += ss.Resorted
	}
	if opts.DryRun || moved == 0 {
		return nil
	}
	return s.lib.ReorderRadioChannels(ctx, r.ID, order)
}

// Resorted is how many channels the sync moved in all.
func (r SyncResult) Resorted() int {
	n := 0
	for _, s := range r.Sources {
		n += s.Resorted
	}
	return n
}

// SourceLabel describes a source for messages: its number and query or
// plan.
func SourceLabel(src domain.RadioSource) string {
	if src.PlanID != "" {
		return fmt.Sprintf("Source %d (plan %s)", src.Position+1, src.PlanName)
	}
	return fmt.Sprintf("Source %d (%s)", src.Position+1, strings.TrimSpace(src.Query))
}

// syncPlan makes a plan source's part of the radio follow the plan: the
// plan's channels, linked into the library where they aren't yet, in the
// plan's order, named as the plan names them, and numbered from the
// source's section start as the plan numbers them. A plan's channel
// already elsewhere on the radio moves into the plan's part; channels the
// plan dropped come off. Channels outside the radio's bands or in modes it
// lacks are left out unless opts.AnyBand or opts.AnyMode. An archived
// plan's channels stay as they are.
func (s *Service) syncPlan(ctx context.Context, r domain.Radio, m model.Model, src domain.RadioSource, claimed map[string]bool, opts SyncOptions) (SourceSync, error) {
	ss := SourceSync{Source: src}
	es, err := s.lib.RadioEntries(ctx, r.ID)
	if err != nil {
		return ss, err
	}
	sh, err := s.shared(ctx)
	if err != nil {
		return ss, err
	}
	p, err := sh.lib.GetPlan(ctx, src.PlanID)
	if err != nil {
		return ss, err
	}
	if p.Status == domain.PlanArchived {
		ss.Note = "the plan is archived, so its channels stay as they are"
		return ss, nil
	}
	rows, err := s.PlanRows(ctx, p)
	if err != nil {
		return ss, fmt.Errorf("plan %s: %w", p.Name, err)
	}
	cs := make([]domain.Channel, len(rows))
	for i, row := range rows {
		cs[i] = row.Channel
	}
	var fit AddResult
	fit.fit(m, cs, opts.AnyBand, opts.AnyMode)
	ss.Skipped, ss.WrongMode = fit.Skipped, fit.WrongMode
	fits := map[string]bool{}
	for _, c := range fit.Add {
		fits[c.ID] = true
	}
	// want is the source's part of the radio as the plan has it: shared
	// channels, by their IDs.
	type want struct {
		id   string
		row  PlanRow
		name string
	}
	var wants []want
	wanted := map[string]bool{}
	for _, row := range rows {
		if !fits[row.Channel.ID] {
			continue
		}
		id := row.Channel.ID
		name := ""
		if p.Entries[row.Entry].Name != "" {
			name = row.Name
		}
		wants = append(wants, want{id, row, name})
		wanted[id] = true
	}
	var off []string
	ours := map[string]bool{}
	for _, e := range es {
		switch {
		case e.SourceID == src.ID && !wanted[e.Channel.ID]:
			ss.Removed = append(ss.Removed, e.Channel)
			off = append(off, e.Channel.ID)
		case e.SourceID == src.ID:
			ours[e.Channel.ID] = true
		case wanted[e.Channel.ID]:
			// Elsewhere on the radio: it moves into the plan's part.
			off = append(off, e.Channel.ID)
		}
	}
	var add []string
	for _, w := range wants {
		claimed[w.id] = true
		if !ours[w.id] {
			add = append(add, w.id)
			ss.Add = append(ss.Add, w.row.Channel)
		}
	}
	if opts.DryRun {
		return ss, nil
	}
	if len(off) > 0 {
		if _, err := s.lib.RemoveRadioChannels(ctx, r.ID, off); err != nil {
			return ss, err
		}
	}
	if len(add) > 0 {
		if ss.Added, err = s.lib.AddSourceChannels(ctx, src, add); err != nil {
			return ss, err
		}
	}
	// Put the source's channels in the plan's order, in the places they
	// hold.
	if es, err = s.lib.RadioEntries(ctx, r.ID); err != nil {
		return ss, err
	}
	order := make([]string, len(es))
	k := 0
	for i, e := range es {
		order[i] = e.Channel.ID
		if e.SourceID == src.ID && k < len(wants) {
			if order[i] != wants[k].id && ours[order[i]] {
				ss.Resorted++
			}
			order[i] = wants[k].id
			k++
		}
	}
	if err := s.lib.ReorderRadioChannels(ctx, r.ID, order); err != nil {
		return ss, err
	}
	if es, err = s.lib.RadioEntries(ctx, r.ID); err != nil {
		return ss, err
	}
	k, prev := 0, 0
	for _, e := range es {
		if e.SourceID != src.ID || k >= len(wants) {
			continue
		}
		w := wants[k]
		var section *int
		if src.SectionStart != nil && (k == 0 || w.row.Number != prev+1) {
			at := *src.SectionStart + w.row.Number - 1
			section = &at
		}
		prev = w.row.Number
		k++
		if e.Name == w.name && equalInt(e.SectionStart, section) {
			continue
		}
		e.Name, e.SectionStart = w.name, section
		if err := s.lib.SetRadioEntry(ctx, r.ID, e); err != nil {
			return ss, err
		}
	}
	return ss, nil
}

func equalInt(a, b *int) bool { return a == nil && b == nil || a != nil && b != nil && *a == *b }
