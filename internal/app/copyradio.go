package app

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/ryan/rflib/internal/domain"
	"github.com/ryan/rflib/internal/model"
	"github.com/ryan/rflib/internal/store"
)

// CopyResult is what copying a radio did.
type CopyResult struct {
	Radio domain.Radio
	// Copied is how many channels came across, and Skipped those the new
	// radio's model can't use (outside its bands, or a mode it lacks).
	Copied  int
	Skipped []domain.Channel
	// LayoutErr is set when the copy doesn't lay out on the new model, such
	// as a section starting past its last memory; the copy is made, to be
	// adjusted.
	LayoutErr error
}

// CopyRadio makes a new radio from another as it is now: its sections, in
// order, with their starts, sizes, and orders; its sources, feeding the
// matching sections; and its channels, in their sections and order, with
// the radio's overrides. to gives the new radio's name and model (and any
// other settings; empty ones come from the original). Channels the new
// model can't use are left out. From then on, the two are independent.
func (s *Service) CopyRadio(ctx context.Context, from string, to domain.Radio) (CopyResult, error) {
	var res CopyResult
	if err := s.writable(); err != nil {
		return res, err
	}
	src, err := s.lib.GetRadio(ctx, from)
	if err != nil {
		return res, err
	}
	m, err := model.Find(to.Model)
	if err != nil {
		return res, err
	}
	if to.DefaultPower == "" {
		to.DefaultPower = src.DefaultPower
	}
	if to.Dedupe == "" {
		to.Dedupe = src.Dedupe
	}
	if to.Notes == "" {
		to.Notes = src.Notes
	}
	sections, err := s.lib.RadioSections(ctx, src.ID)
	if err != nil {
		return res, err
	}
	sources, err := s.lib.RadioSources(ctx, src.ID)
	if err != nil {
		return res, err
	}
	entries, err := s.lib.RadioEntries(ctx, src.ID)
	if err != nil {
		return res, err
	}
	err = s.lib.InTx(ctx, func(tx store.Library) error {
		r, err := tx.CreateRadio(ctx, to)
		if err != nil {
			return err
		}
		res.Radio = r
		// Sections: the default takes the original default's shape; the
		// others are added, then all put in the original's order.
		made, err := tx.RadioSections(ctx, r.ID)
		if err != nil {
			return err
		}
		def := made[0]
		section := map[string]string{}
		for _, sec := range sections {
			if sec.Default {
				def.Name, def.Start, def.Size, def.Sort = sec.Name, sec.Start, sec.Size, sec.Sort
				if err := tx.UpdateRadioSection(ctx, def); err != nil {
					return err
				}
				section[sec.ID] = def.ID
				continue
			}
			copied, err := tx.CreateRadioSection(ctx, domain.RadioSection{RadioID: r.ID, Name: sec.Name, Start: sec.Start, Size: sec.Size, Sort: sec.Sort})
			if err != nil {
				return err
			}
			section[sec.ID] = copied.ID
		}
		for i, sec := range sections {
			if err := tx.MoveRadioSection(ctx, r.ID, section[sec.ID], i); err != nil {
				return err
			}
		}
		source := map[string]domain.RadioSource{}
		for _, x := range sources {
			copied, err := tx.AddRadioSource(ctx, domain.RadioSource{RadioID: r.ID, SectionID: section[x.SectionID], Query: x.Query,
				PlanID: x.PlanID, Sort: x.Sort, PlanNumbers: x.PlanNumbers})
			if err != nil {
				return err
			}
			source[x.ID] = copied
		}
		// Channels, one at a time in list order, so each section keeps its
		// order, and each keeps its source and overrides.
		for _, e := range entries {
			if !m.Receives(e.Channel) {
				res.Skipped = append(res.Skipped, e.Channel)
				continue
			}
			ids := []string{e.Channel.ID}
			if x, ok := source[e.SourceID]; ok {
				// Its source's, in the section it's in, which may not be
				// the source's own.
				x.SectionID = section[e.Section.ID]
				_, err = tx.AddSourceChannels(ctx, x, ids)
			} else {
				_, err = tx.AddRadioChannels(ctx, r.ID, section[e.Section.ID], ids)
			}
			if err != nil {
				return fmt.Errorf("%s: %w", e.Channel.Name, err)
			}
			if err := tx.SetRadioEntry(ctx, r.ID, e); err != nil {
				return fmt.Errorf("%s: %w", e.Channel.Name, err)
			}
			res.Copied++
		}
		if _, _, err := layoutOf(ctx, tx, r, m); err != nil {
			res.LayoutErr = err
		}
		return nil
	})
	return res, err
}

// RadioComparison is a radio against another, by channel: how many both
// have, what only the other has (Missing; Unusable when this radio's model
// can't use it), and what only this one has (Extra).
type RadioComparison struct {
	Radio, Other domain.Radio
	Same         int
	Missing      []MissingChannel
	Extra        []domain.Channel
}

// MissingChannel is a channel the other radio has and this one doesn't:
// the other's entry for it (its section and overrides), and why this radio
// can't use it, if it can't.
type MissingChannel struct {
	Entry    domain.RadioEntry
	Unusable string
}

// CompareRadios compares a radio with another, by channel.
func (s *Service) CompareRadios(ctx context.Context, radio, other string) (RadioComparison, error) {
	var c RadioComparison
	r, m, err := s.RadioModel(ctx, radio)
	if err != nil {
		return c, err
	}
	o, err := s.lib.GetRadio(ctx, other)
	if err != nil {
		return c, err
	}
	if o.ID == r.ID {
		return c, domain.Invalidf("compare %s with another radio", r.Name)
	}
	c.Radio, c.Other = r, o
	mine, err := s.lib.RadioEntries(ctx, r.ID)
	if err != nil {
		return c, err
	}
	theirs, err := s.lib.RadioEntries(ctx, o.ID)
	if err != nil {
		return c, err
	}
	have, has := map[string]bool{}, map[string]bool{}
	for _, e := range mine {
		have[e.Channel.ID] = true
	}
	for _, e := range theirs {
		has[e.Channel.ID] = true
		switch {
		case have[e.Channel.ID]:
			c.Same++
		case !m.Tunes(e.Channel.ReceiveHz):
			c.Missing = append(c.Missing, MissingChannel{e, fmt.Sprintf("%s MHz is outside the %s's bands", domain.FormatFrequencyMHz(e.Channel.ReceiveHz), m.Description)})
		case !m.HasMode(e.Channel.Mode):
			c.Missing = append(c.Missing, MissingChannel{e, fmt.Sprintf("the %s doesn't have %s", m.Description, e.Channel.Mode)})
		default:
			c.Missing = append(c.Missing, MissingChannel{Entry: e})
		}
	}
	for _, e := range mine {
		if !has[e.Channel.ID] {
			c.Extra = append(c.Extra, e.Channel)
		}
	}
	return c, nil
}

// AddFromResult is what adding from another radio did (or would): the
// channels added, the sections made for them, those of the sections made
// whose start didn't fit here, so they follow the section before them, and
// the existing sections whose new channels would have run into a later
// section's start, so went to a section of their own at the end instead.
type AddFromResult struct {
	Added       []domain.Channel
	NewSections []string
	Unstarted   []string
	Overflowed  []string
}

// AddFromRadio adds channels another radio has and this one doesn't (by
// ID, or all usable ones when ids is empty), in the other's order, each
// into this radio's section of the same name (ignoring case), with the
// other radio's overrides for it. A section this radio lacks is made, as
// the other radio has it (start, size, and order), at the end, after this
// radio's sections, so none of them, nor their memory numbers, change;
// when its start doesn't fit there, it's made without one. New channels
// that would push one of this radio's sections into a later section's
// start go to a section of their own at the end instead, named for where
// they came from. Reorder the sections afterwards as you like. With dryRun it works everything out,
// then changes nothing.
func (s *Service) AddFromRadio(ctx context.Context, radio, other string, ids []string, dryRun bool) (RadioComparison, AddFromResult, error) {
	var res AddFromResult
	c, err := s.CompareRadios(ctx, radio, other)
	if err != nil {
		return c, res, err
	}
	want := map[string]bool{}
	for _, ref := range ids {
		id, err := s.ChannelID(ctx, ref)
		if err != nil {
			return c, res, err
		}
		want[id] = true
	}
	var pick []domain.RadioEntry
	for _, x := range c.Missing {
		if len(ids) > 0 && !want[x.Entry.Channel.ID] {
			continue
		}
		if x.Unusable != "" {
			if len(ids) > 0 {
				return c, res, domain.Invalidf("%s: %s", x.Entry.Channel.Name, x.Unusable)
			}
			continue
		}
		delete(want, x.Entry.Channel.ID)
		pick = append(pick, x.Entry)
	}
	for id := range want {
		return c, res, domain.Invalidf("channel %s isn't one %s has and %s doesn't", id, c.Other.Name, c.Radio.Name)
	}
	for _, e := range pick {
		res.Added = append(res.Added, e.Channel)
	}
	if len(pick) == 0 {
		return c, res, nil
	}
	theirs, err := s.lib.RadioSections(ctx, c.Other.ID)
	if err != nil {
		return c, res, err
	}
	err = s.lib.InTx(ctx, func(tx store.Library) error {
		var made []domain.RadioSection
		// section returns this radio's section of a name, made at the end
		// when there isn't one.
		section := func(name string) (string, error) {
			ours, err := tx.RadioSections(ctx, c.Radio.ID)
			if err != nil {
				return "", err
			}
			for _, sec := range ours {
				if strings.EqualFold(sec.Name, name) {
					return sec.ID, nil
				}
			}
			k := slices.IndexFunc(theirs, func(x domain.RadioSection) bool { return strings.EqualFold(x.Name, name) })
			if k < 0 {
				return "", nil // gone from the other radio meanwhile: the default
			}
			to := len(ours)
			x := theirs[k]
			sec, err := tx.CreateRadioSection(ctx, domain.RadioSection{RadioID: c.Radio.ID, Name: x.Name, Start: x.Start, Size: x.Size, Sort: x.Sort})
			if err != nil {
				return "", err
			}
			if err := tx.MoveRadioSection(ctx, c.Radio.ID, sec.ID, to); err != nil {
				return "", err
			}
			made = append(made, sec)
			res.NewSections = append(res.NewSections, sec.Name)
			return sec.ID, nil
		}
		// Channels added to sections this radio had, by section, in order.
		existing := map[string]bool{}
		if ours, err := tx.RadioSections(ctx, c.Radio.ID); err != nil {
			return err
		} else {
			for _, sec := range ours {
				existing[sec.ID] = true
			}
		}
		var grown []string
		added := map[string][]domain.RadioEntry{}
		for _, e := range pick {
			into, err := section(e.Section.Name)
			if err != nil {
				return err
			}
			if existing[into] {
				if _, ok := added[into]; !ok {
					grown = append(grown, into)
				}
				added[into] = append(added[into], e)
			}
			if _, err := tx.AddRadioChannels(ctx, c.Radio.ID, into, []string{e.Channel.ID}); err != nil {
				return fmt.Errorf("%s: %w", e.Channel.Name, err)
			}
			e.PlanNumber = nil // a plan's number is its source's, on the other radio
			if err := tx.SetRadioEntry(ctx, c.Radio.ID, e); err != nil {
				return fmt.Errorf("%s: %w", e.Channel.Name, err)
			}
		}
		// A section of this radio's that its new channels push into a later
		// start gives them up, to a section of their own at the end.
		err := fits(ctx, tx, c.Radio)
		for _, id := range grown {
			if err == nil {
				break
			}
			ours, lerr := tx.RadioSections(ctx, c.Radio.ID)
			if lerr != nil {
				return lerr
			}
			var name string
			for _, sec := range ours {
				if sec.ID == id {
					name = sec.Name
				}
			}
			spill, cerr := tx.CreateRadioSection(ctx, domain.RadioSection{RadioID: c.Radio.ID, Name: fmt.Sprintf("%s (from %s)", name, c.Other.Name)})
			if cerr != nil {
				return cerr
			}
			if cerr := tx.MoveRadioSection(ctx, c.Radio.ID, spill.ID, len(ours)); cerr != nil {
				return cerr
			}
			for _, e := range added[id] {
				if merr := tx.MoveRadioChannel(ctx, c.Radio.ID, e.Channel.ID, spill.ID, len(added[id])); merr != nil {
					return merr
				}
			}
			res.Overflowed = append(res.Overflowed, name)
			res.NewSections = append(res.NewSections, spill.Name)
			err = fits(ctx, tx, c.Radio)
		}
		// A start that doesn't fit here goes, section by section, until the
		// radio lays out.
		for _, sec := range made {
			if err == nil {
				break
			}
			if sec.Start == nil {
				continue
			}
			sec.Start = nil
			if err := tx.UpdateRadioSection(ctx, sec); err != nil {
				return err
			}
			res.Unstarted = append(res.Unstarted, sec.Name)
			err = fits(ctx, tx, c.Radio)
		}
		if err != nil {
			return err
		}
		if dryRun {
			return errDryRun
		}
		return nil
	})
	if errors.Is(err, errDryRun) {
		err = nil
	}
	return c, res, err
}
