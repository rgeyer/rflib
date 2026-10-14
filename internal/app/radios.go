package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/ryan/rflib/internal/domain"
	"github.com/ryan/rflib/internal/model"
	"github.com/ryan/rflib/internal/program"
	"github.com/ryan/rflib/internal/query"
	"github.com/ryan/rflib/internal/store"
)

// CreateRadio adds a radio of a known model.
func (s *Service) CreateRadio(ctx context.Context, r domain.Radio) (domain.Radio, error) {
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

// Plan plans a radio's memories (see program.Build). The entries are in
// the same order as the plan's memories.
func (s *Service) Plan(ctx context.Context, idOrName string) (program.Plan, []domain.RadioEntry, error) {
	r, m, err := s.RadioModel(ctx, idOrName)
	if err != nil {
		return program.Plan{}, nil, err
	}
	es, err := s.lib.RadioEntries(ctx, r.ID)
	if err != nil {
		return program.Plan{}, nil, err
	}
	p, err := program.Build(r, m, es)
	return p, es, err
}

// replan reports an error when a change leaves a radio unable to export,
// such as a section start behind the channels before it.
func (s *Service) replan(ctx context.Context, r domain.Radio) error {
	if _, _, err := s.Plan(ctx, r.ID); err != nil {
		return fmt.Errorf("saved, but %s no longer exports: %w", r.Name, err)
	}
	return nil
}

// AddRequest is what to add to a radio: channels by ID, then the matches
// of Query in Sort order.
type AddRequest struct {
	IDs   []string
	Query query.Node
	Sort  string
	// AnyBand adds channels the radio can't receive, too.
	AnyBand bool
}

// AddResult is what an add or a source's sync did. Add lists what was (or
// would be) added, Skipped what the radio can't receive.
type AddResult struct {
	Add     []domain.Channel
	Added   int
	Skipped []domain.Channel
}

// inBand splits channels by whether the model can tune their receive
// frequency. A transmit frequency outside the bands only warns (see
// program.Build), since the radio can still listen.
func inBand(m model.Model, cs []domain.Channel, anyBand bool) (kept, skipped []domain.Channel) {
	if anyBand {
		return cs, nil
	}
	for _, c := range cs {
		if m.Tunes(c.ReceiveHz) {
			kept = append(kept, c)
		} else {
			skipped = append(skipped, c)
		}
	}
	return kept, skipped
}

// AddChannels appends channels to the end of a radio's list. Channels
// already on it are left where they are, and channels outside its bands
// are skipped unless req.AnyBand.
func (s *Service) AddChannels(ctx context.Context, radio string, req AddRequest) (domain.Radio, AddResult, error) {
	r, m, err := s.RadioModel(ctx, radio)
	if err != nil {
		return r, AddResult{}, err
	}
	var cs []domain.Channel
	for _, id := range req.IDs {
		c, err := s.lib.GetChannel(ctx, id)
		if err != nil {
			return r, AddResult{}, err
		}
		cs = append(cs, c)
	}
	if req.Query != nil {
		found, err := s.FindChannels(ctx, req.Query)
		if err != nil {
			return r, AddResult{}, err
		}
		if err := domain.SortChannels(found, orDefault(req.Sort, "name")); err != nil {
			return r, AddResult{}, err
		}
		cs = append(cs, found...)
	}
	var res AddResult
	res.Add, res.Skipped = inBand(m, cs, req.AnyBand)
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
func (s *Service) RemoveChannels(ctx context.Context, radio string, ids []string) (domain.Radio, int, error) {
	r, err := s.lib.GetRadio(ctx, radio)
	if err != nil {
		return r, 0, err
	}
	n, err := s.lib.RemoveRadioChannels(ctx, r.ID, ids)
	return r, n, err
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
		for _, e := range es {
			if e.Channel.ID == ref {
				return e.Position, nil
			}
		}
		return 0, fmt.Errorf("channel %q is not on %s: %w", ref, r.Name, store.ErrNotFound)
	}
	from, err := index(channelID)
	if err != nil {
		return err
	}
	var to int
	switch {
	case mv.Top:
		to = 0
	case mv.Bottom:
		to = len(es)
	case mv.To != 0:
		if mv.To < 1 {
			return fmt.Errorf("positions count from 1")
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
		return fmt.Errorf("say where to move the channel")
	}
	return s.lib.MoveRadioChannel(ctx, r.ID, channelID, to)
}

// RadioEntry returns a radio's entry for one of its channels.
func (s *Service) RadioEntry(ctx context.Context, radio, channelID string) (domain.Radio, domain.RadioEntry, error) {
	r, es, err := s.RadioEntries(ctx, radio)
	if err != nil {
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
// an error if the radio no longer plans, such as a section start behind
// the channels before it. The overrides are saved either way.
func (s *Service) SetEntry(ctx context.Context, radio string, e domain.RadioEntry) error {
	r, err := s.lib.GetRadio(ctx, radio)
	if err != nil {
		return err
	}
	if err := s.lib.SetRadioEntry(ctx, r.ID, e); err != nil {
		return err
	}
	return s.replan(ctx, r)
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
		return r, domain.RadioSource{}, fmt.Errorf("%s has sources 1-%d; see rflib radio sources list %s", r.Name, len(sources), r.Name)
	}
	return r, sources[number-1], nil
}

// AddSource appends a source to a radio after checking its query.
func (s *Service) AddSource(ctx context.Context, radio string, src domain.RadioSource) (domain.Radio, domain.RadioSource, error) {
	r, err := s.lib.GetRadio(ctx, radio)
	if err != nil {
		return r, src, err
	}
	if _, err := s.CheckQuery(ctx, src.Query, nil); err != nil {
		return r, src, err
	}
	src.RadioID, src.Sort = r.ID, orDefault(src.Sort, "name")
	src, err = s.lib.AddRadioSource(ctx, src)
	return r, src, err
}

// UpdateSource saves a changed source after checking its query, and
// reports an error if the radio no longer plans.
func (s *Service) UpdateSource(ctx context.Context, src domain.RadioSource) error {
	r, err := s.lib.GetRadio(ctx, src.RadioID)
	if err != nil {
		return err
	}
	if _, err := s.CheckQuery(ctx, src.Query, nil); err != nil {
		return err
	}
	if err := s.lib.UpdateRadioSource(ctx, src); err != nil {
		return err
	}
	return s.replan(ctx, r)
}

// MoveSource moves a radio's source number from to number to (both from
// 1).
func (s *Service) MoveSource(ctx context.Context, radio string, from, to int) error {
	if to < 1 {
		return fmt.Errorf("source numbers count from 1")
	}
	_, src, err := s.Source(ctx, radio, from)
	if err != nil {
		return err
	}
	return s.lib.MoveRadioSource(ctx, src, to-1)
}

// RemoveSource deletes a radio's source by number. Its channels stay unless
// withChannels; it returns how many were taken off.
func (s *Service) RemoveSource(ctx context.Context, radio string, number int, withChannels bool) (int, error) {
	_, src, err := s.Source(ctx, radio, number)
	if err != nil {
		return 0, err
	}
	return s.lib.RemoveRadioSource(ctx, src, withChannels)
}

// SyncResult is what a radio sync did, source by source. PlanErr is set
// when the radio no longer plans afterwards, such as a source running past
// the next section start.
type SyncResult struct {
	Radio   domain.Radio
	Sources []SourceSync
	PlanErr error
}

type SourceSync struct {
	Source domain.RadioSource
	AddResult
}

// Added is how many channels the sync added in all.
func (r SyncResult) Added() int {
	n := 0
	for _, s := range r.Sources {
		n += s.Added
	}
	return n
}

// Sync adds each source's new matches to a radio (see AddSourceChannels).
// An earlier source claims a channel first; channels the radio can't
// receive are skipped, and stay unclaimed, unless anyBand. With dryRun
// nothing changes and Add lists what would be added.
func (s *Service) Sync(ctx context.Context, radio string, dryRun, anyBand bool) (SyncResult, error) {
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
		return res, fmt.Errorf("%s has no sources; add one with: rflib radio sources add %s QUERY", r.Name, r.Name)
	}
	claimed, err := s.lib.RadioChannelIDs(ctx, r.ID)
	if err != nil {
		return res, err
	}
	for _, src := range sources {
		cs, err := s.Find(ctx, src.Query)
		if err != nil {
			return res, fmt.Errorf("source %d: %w", src.Position+1, err)
		}
		if err := domain.SortChannels(cs, src.Sort); err != nil {
			return res, err
		}
		var unclaimed []domain.Channel
		for _, c := range cs {
			if !claimed[c.ID] {
				unclaimed = append(unclaimed, c)
			}
		}
		ss := SourceSync{Source: src}
		ss.Add, ss.Skipped = inBand(m, unclaimed, anyBand)
		ids := make([]string, len(ss.Add))
		for i, c := range ss.Add {
			claimed[c.ID] = true
			ids[i] = c.ID
		}
		if !dryRun && len(ids) > 0 {
			if ss.Added, err = s.lib.AddSourceChannels(ctx, src, ids); err != nil {
				return res, err
			}
		}
		res.Sources = append(res.Sources, ss)
	}
	if !dryRun {
		if _, _, err := s.Plan(ctx, r.ID); err != nil {
			res.PlanErr = err
		}
	}
	return res, nil
}

// SourceLabel describes a source for messages: its number and query.
func SourceLabel(src domain.RadioSource) string {
	return fmt.Sprintf("Source %d (%s)", src.Position+1, strings.TrimSpace(src.Query))
}
