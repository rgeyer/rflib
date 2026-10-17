package app

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/ryan/rflib/internal/domain"
	"github.com/ryan/rflib/internal/importer"
	"github.com/ryan/rflib/internal/seed"
	"github.com/ryan/rflib/internal/store"
)

// Built-in lists seed the shared list. Each seeded channel records its list,
// its name there, and the row as last applied (domain.SeedChannel), so an
// update applies only what the list changed and keeps a maintainer's
// corrections.

// SeedChange is one change a built-in list's update makes to the shared
// list.
type SeedChange struct {
	// Kind is add, update, or drop.
	Kind string
	Name string
	// Channel is the list's row for add and update, with the shared
	// channel's ID for update, and the shared channel for drop.
	Channel domain.Channel
	// Fields are what an update changes: fields the list changed since it
	// was last applied. Kept are fields the list changed that a maintainer
	// had corrected; the corrections stay.
	Fields, Kept []string
}

// SeedStatus is a built-in list against the shared list: whether it's
// installed, and the changes an update would make.
type SeedStatus struct {
	List      seed.List
	Installed bool
	Changes   []SeedChange
	// records are rows already as the list has them, recorded on update
	// without changing their channels.
	records []domain.SeedChannel
}

// Pending reports whether an update would change the shared list.
func (st SeedStatus) Pending() bool { return len(st.Changes) > 0 }

// seedField is a shared field a built-in list sets.
type seedField struct {
	name string
	get  func(domain.Channel) string
	set  func(dst *domain.Channel, src domain.Channel)
}

var seedFields = []seedField{
	{"name", func(c domain.Channel) string { return c.Name }, func(d *domain.Channel, s domain.Channel) { d.Name = s.Name }},
	{"rx", func(c domain.Channel) string { return fmt.Sprint(c.ReceiveHz) }, func(d *domain.Channel, s domain.Channel) { d.ReceiveHz = s.ReceiveHz }},
	{"tx", func(c domain.Channel) string {
		if c.TransmitHz == nil {
			return ""
		}
		return fmt.Sprint(*c.TransmitHz)
	}, func(d *domain.Channel, s domain.Channel) { d.TransmitHz = s.TransmitHz }},
	{"mode", func(c domain.Channel) string { return c.Mode }, func(d *domain.Channel, s domain.Channel) { d.Mode = s.Mode }},
	{"service", func(c domain.Channel) string { return string(c.Service) }, func(d *domain.Channel, s domain.Channel) { d.Service = s.Service }},
	{"rx only", func(c domain.Channel) string { return fmt.Sprint(c.RXOnly) }, func(d *domain.Channel, s domain.Channel) { d.RXOnly = s.RXOnly }},
	{"receive tone", func(c domain.Channel) string { return domain.FormatTone(c.ReceiveTone) }, func(d *domain.Channel, s domain.Channel) { d.ReceiveTone = s.ReceiveTone }},
	{"transmit tone", func(c domain.Channel) string { return domain.FormatTone(c.TransmitTone) }, func(d *domain.Channel, s domain.Channel) { d.TransmitTone = s.TransmitTone }},
	{"tone squelch", func(c domain.Channel) string { return fmt.Sprint(c.ToneSquelch) }, func(d *domain.Channel, s domain.Channel) { d.ToneSquelch = s.ToneSquelch }},
	{"bandwidth", func(c domain.Channel) string { return c.Bandwidth }, func(d *domain.Channel, s domain.Channel) { d.Bandwidth = s.Bandwidth }},
	{"location", func(c domain.Channel) string {
		if c.Location == nil {
			return ""
		}
		l := *c.Location
		if l.RadiusKM == nil {
			return fmt.Sprintf("%g,%g", l.Latitude, l.Longitude)
		}
		return fmt.Sprintf("%g,%g,%g", l.Latitude, l.Longitude, *l.RadiusKM)
	}, func(d *domain.Channel, s domain.Channel) { d.Location = s.Location }},
	{"tags", func(c domain.Channel) string { return strings.Join(domain.NormalizeTags(c.Tags), ",") }, func(d *domain.Channel, s domain.Channel) { d.Tags = s.Tags }},
	{"notes", func(c domain.Channel) string { return strings.TrimSpace(c.Notes) }, func(d *domain.Channel, s domain.Channel) { d.Notes = s.Notes }},
}

// SeedStatus compares a built-in list with the shared list.
func (s *Service) SeedStatus(ctx context.Context, l seed.List) (SeedStatus, error) {
	sh, err := s.sharedLibrary(ctx)
	if err != nil {
		return SeedStatus{List: l}, err
	}
	b, err := l.Batch()
	if err != nil {
		return SeedStatus{List: l}, err
	}
	return planSeed(ctx, sh, l, b.Records)
}

// RecordSeeds records, for every built-in list, the shared channels already
// as the list has them, without changing any channel, so later updates can
// tell what a list dropped. Upgrading runs it for lists seeded before rows
// were recorded. Run on the shared library.
func (s *Service) RecordSeeds(ctx context.Context) error {
	if !s.info.Shared {
		return errNotShared
	}
	if err := maintaining(ctx); err != nil {
		return err
	}
	for _, l := range seed.Lists() {
		st, err := s.SeedStatus(ctx, l)
		if err != nil {
			return fmt.Errorf("%s: %w", l.ID, err)
		}
		for _, r := range st.records {
			if err := s.lib.SetSeedChannel(ctx, r); err != nil {
				return err
			}
		}
	}
	return nil
}

// UpdateSeed brings the shared list up to date with a built-in list:
// adding its new rows, applying what it changed (but not over a
// maintainer's correction), and retiring channels it dropped, each with the
// list as the revision's reason. Run on the shared library. With dryRun it
// only reports the changes.
func (s *Service) UpdateSeed(ctx context.Context, l seed.List, dryRun bool) (SeedStatus, error) {
	if !s.info.Shared {
		return SeedStatus{List: l}, errNotShared
	}
	if err := maintaining(ctx); err != nil {
		return SeedStatus{List: l}, err
	}
	b, err := l.Batch()
	if err != nil {
		return SeedStatus{List: l}, err
	}
	return s.updateSeed(ctx, l, b.Records, dryRun)
}

// updateSeed is UpdateSeed with the list's rows given.
func (s *Service) updateSeed(ctx context.Context, l seed.List, rows []importer.Record, dryRun bool) (SeedStatus, error) {
	st, err := planSeed(ctx, s.lib, l, rows)
	if err != nil || dryRun {
		return st, err
	}
	wctx := store.WithChange(ctx, "built-in list "+l.ID, "")
	err = s.lib.InTx(wctx, func(tx store.Library) error {
		record := func(id string, row domain.Channel) error {
			return tx.SetSeedChannel(wctx, domain.SeedChannel{ChannelID: id, List: l.ID, Name: row.Name, Applied: row})
		}
		for _, ch := range st.Changes {
			switch ch.Kind {
			case "add":
				made, err := tx.CreateChannel(wctx, ch.Channel)
				if err != nil {
					return fmt.Errorf("%s: %w", ch.Name, err)
				}
				if err := record(made.ID, ch.Channel); err != nil {
					return err
				}
			case "update":
				cur, err := tx.GetChannel(wctx, ch.Channel.ID)
				if err != nil {
					return err
				}
				for _, f := range seedFields {
					if slices.Contains(ch.Fields, f.name) {
						f.set(&cur, ch.Channel)
					}
				}
				if _, err := tx.UpdateChannel(wctx, cur); err != nil {
					return fmt.Errorf("%s: %w", ch.Name, err)
				}
				if err := record(cur.ID, ch.Channel); err != nil {
					return err
				}
			case "drop":
				cur := ch.Channel
				cur.Status, cur.StatusNote, cur.BackOn = domain.StatusRetired, "dropped from the built-in list "+l.ID, nil
				if _, err := tx.UpdateChannel(wctx, cur); err != nil {
					return fmt.Errorf("%s: %w", ch.Name, err)
				}
			}
		}
		for _, r := range st.records {
			if err := tx.SetSeedChannel(wctx, r); err != nil {
				return err
			}
		}
		return nil
	})
	if err == nil {
		st.Installed = true
	}
	return st, err
}

// planSeed works out what updating the shared list from a built-in list
// would change. A row is the list's channel with its built-in ID (see
// SeedID). A shared channel seeded before rows were recorded counts as
// last applied the way it is now.
func planSeed(ctx context.Context, sh store.Library, l seed.List, rows []importer.Record) (SeedStatus, error) {
	st := SeedStatus{List: l}
	b := importer.Batch{Records: rows}
	curateTags(&b)
	all, err := sh.SeedChannels(ctx)
	if err != nil {
		return st, err
	}
	recorded := map[string]domain.SeedChannel{}
	for _, r := range all {
		if r.List == l.ID {
			recorded[r.Name] = r
			st.Installed = true
		}
	}
	listed := map[string]bool{}
	for _, r := range b.Records {
		row := r.Channel
		listed[row.Name] = true
		rec, has := recorded[row.Name]
		row.ID = SeedID(l.ID, row.Name)
		if has {
			row.ID = rec.ChannelID
		}
		cur, err := sh.GetChannel(ctx, row.ID)
		// A row the list renamed is the channel it had under an old name.
		for _, old := range l.Renamed[row.Name] {
			if has || !errors.Is(err, store.ErrNotFound) {
				break
			}
			listed[old] = true
			if rec, has = recorded[old]; has {
				row.ID = rec.ChannelID
			} else {
				row.ID = SeedID(l.ID, old)
			}
			cur, err = sh.GetChannel(ctx, row.ID)
		}
		if errors.Is(err, store.ErrNotFound) {
			row.ID = SeedID(l.ID, row.Name)
		}
		if errors.Is(err, store.ErrNotFound) {
			st.Changes = append(st.Changes, SeedChange{Kind: "add", Name: row.Name, Channel: row})
			continue
		} else if err != nil {
			return st, err
		}
		st.Installed = true
		applied := cur
		if has {
			applied = rec.Applied
		}
		var fields, kept []string
		for _, f := range seedFields {
			was, now, mine := f.get(applied), f.get(row), f.get(cur)
			switch {
			case was == now || mine == now:
			case mine == was:
				fields = append(fields, f.name)
			default:
				kept = append(kept, f.name)
			}
		}
		if len(fields)+len(kept) > 0 {
			st.Changes = append(st.Changes, SeedChange{Kind: "update", Name: row.Name, Channel: row, Fields: fields, Kept: kept})
		} else if !has || differs(rec.Applied, row) {
			st.records = append(st.records, domain.SeedChannel{ChannelID: row.ID, List: l.ID, Name: row.Name, Applied: row})
		}
	}
	for _, name := range slices.Sorted(maps.Keys(recorded)) {
		rec := recorded[name]
		if listed[name] {
			continue
		}
		cur, err := sh.GetChannel(ctx, rec.ChannelID)
		if errors.Is(err, store.ErrNotFound) {
			continue
		} else if err != nil {
			return st, err
		}
		if cur.Status != domain.StatusRetired {
			st.Changes = append(st.Changes, SeedChange{Kind: "drop", Name: name, Channel: cur})
		}
	}
	return st, nil
}

// differs reports whether two rows differ in any field a list sets.
func differs(a, b domain.Channel) bool {
	for _, f := range seedFields {
		if f.get(a) != f.get(b) {
			return true
		}
	}
	return false
}
