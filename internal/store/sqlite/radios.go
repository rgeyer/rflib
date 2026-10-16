package sqlite

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ryan/rflib/internal/domain"
	"github.com/ryan/rflib/internal/store"
)

const radioColumns = `id,name,model,default_power,dedupe,name_length,notes,created_at,updated_at`

func (l *library) CreateRadio(ctx context.Context, r domain.Radio) (domain.Radio, error) {
	if err := domain.ValidateRadio(r); err != nil {
		return r, err
	}
	if err := assignIdentity(&r.ID, &r.CreatedAt, &r.UpdatedAt); err != nil {
		return r, err
	}
	return r, l.inTx(ctx, func(tx *library) error {
		_, err := tx.q.ExecContext(ctx, `INSERT INTO radios (library_id,`+radioColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?)`,
			tx.id, r.ID, r.Name, r.Model, string(r.DefaultPower), r.Dedupe, r.NameLength, r.Notes, timestamp(r.CreatedAt), timestamp(r.UpdatedAt))
		if err != nil {
			return conflict(fmt.Errorf("create radio: %w", err), "a radio named %q", r.Name)
		}
		// Every radio has a default section, for channels added by hand.
		var id string
		if err := assignIdentity(&id, new(time.Time), new(time.Time)); err != nil {
			return err
		}
		_, err = tx.q.ExecContext(ctx, `INSERT INTO radio_sections (`+sectionColumns+`) VALUES (?,?,0,'Other',NULL,NULL,'',1)`, id, r.ID)
		return err
	})
}

func (l *library) GetRadio(ctx context.Context, idOrName string) (domain.Radio, error) {
	r, err := scanRadio(l.q.QueryRowContext(ctx, `SELECT `+radioColumns+` FROM radios WHERE library_id=? AND (id=? OR name=?)`, l.id, idOrName, idOrName))
	if errors.Is(err, sql.ErrNoRows) {
		return r, fmt.Errorf("radio %q: %w", idOrName, store.ErrNotFound)
	}
	return r, err
}

func (l *library) ListRadios(ctx context.Context) ([]domain.Radio, error) {
	rows, err := l.q.QueryContext(ctx, `SELECT `+radioColumns+` FROM radios WHERE library_id=? ORDER BY name`, l.id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Radio
	for rows.Next() {
		r, err := scanRadio(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (l *library) UpdateRadio(ctx context.Context, r domain.Radio) (domain.Radio, error) {
	if err := domain.ValidateRadio(r); err != nil {
		return r, err
	}
	r.UpdatedAt = time.Now().UTC()
	res, err := l.q.ExecContext(ctx, `UPDATE radios SET name=?,model=?,default_power=?,dedupe=?,name_length=?,notes=?,updated_at=? WHERE id=? AND library_id=?`,
		r.Name, r.Model, string(r.DefaultPower), r.Dedupe, r.NameLength, r.Notes, timestamp(r.UpdatedAt), r.ID, l.id)
	if err != nil {
		return r, conflict(fmt.Errorf("update radio: %w", err), "a radio named %q", r.Name)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return r, fmt.Errorf("radio %q: %w", r.ID, store.ErrNotFound)
	}
	return r, nil
}

func (l *library) DeleteRadio(ctx context.Context, id string) error {
	res, err := l.q.ExecContext(ctx, `DELETE FROM radios WHERE id=? AND library_id=?`, id, l.id)
	if err != nil {
		return fmt.Errorf("delete radio: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("radio %q: %w", id, store.ErrNotFound)
	}
	return nil
}

func scanRadio(r scanner) (domain.Radio, error) {
	var x domain.Radio
	var created, updated string
	err := r.Scan(&x.ID, &x.Name, &x.Model, &x.DefaultPower, &x.Dedupe, &x.NameLength, &x.Notes, &created, &updated)
	if err != nil {
		return x, err
	}
	x.CreatedAt, x.UpdatedAt = parseTime(created), parseTime(updated)
	return x, nil
}

func (l *library) ownsRadio(ctx context.Context, radioID string) error {
	return l.owns(ctx, "radios", "radio", radioID)
}

func (l *library) RadioEntries(ctx context.Context, radioID string) ([]domain.RadioEntry, error) {
	if err := l.ownsRadio(ctx, radioID); err != nil {
		return nil, err
	}
	sections, err := listSections(ctx, l.q, radioID)
	if err != nil {
		return nil, err
	}
	byID := map[string]domain.RadioSection{}
	for _, sec := range sections {
		byID[sec.ID] = sec
	}
	rows, err := l.q.QueryContext(ctx, `SELECT rc.channel_id, rc.section_id, rc.name, rc.power, rc.skip, coalesce(rc.source_id,''), rc.plan_number,
		coalesce(src.plan_numbers,''), coalesce(src.position+1, 0) FROM radio_channels rc JOIN radio_sections s ON s.id=rc.section_id LEFT JOIN radio_sources src ON src.id=rc.source_id
		WHERE rc.radio_id=? ORDER BY s.position, rc.position`, radioID)
	if err != nil {
		return nil, fmt.Errorf("list radio channels: %w", err)
	}
	var entries []domain.RadioEntry
	var ids []string
	for rows.Next() {
		var e domain.RadioEntry
		var id, section, numbers string
		var skip sql.NullBool
		var plan sql.NullInt64
		if err := rows.Scan(&id, &section, &e.Name, &e.Power, &skip, &e.SourceID, &plan, &numbers, &e.SourceNumber); err != nil {
			_ = rows.Close()
			return nil, err
		}
		e.Section = byID[section]
		if skip.Valid {
			e.Skip = &skip.Bool
		}
		if plan.Valid {
			v := int(plan.Int64)
			e.PlanNumber = &v
		}
		e.MatchPlan = numbers == domain.PlanNumbersMatch
		entries = append(entries, e)
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	// A radio holds the library's own channels and shared ones, seen
	// through the library's annotations.
	channels, err := l.listChannels(ctx, `c.library_id=? AND c.link_id IS NULL AND c.id IN (SELECT channel_id FROM radio_channels WHERE radio_id=?)`, l.id, radioID)
	if err != nil {
		return nil, err
	}
	byChannel := make(map[string]domain.Channel, len(ids))
	for _, c := range channels {
		byChannel[c.ID] = c
	}
	var shared []string
	for _, id := range ids {
		if _, ok := byChannel[id]; !ok {
			shared = append(shared, id)
		}
	}
	views, err := l.SharedViews(ctx, shared)
	if err != nil {
		return nil, err
	}
	for id, v := range views {
		byChannel[id] = v
	}
	for i, id := range ids {
		entries[i].Channel = byChannel[id]
	}
	// A sorted section's channels are in its order, whatever their places.
	for i := 0; i < len(entries); {
		j := i
		for j < len(entries) && entries[j].Section.ID == entries[i].Section.ID {
			j++
		}
		switch entries[i].Section.Sort {
		case "name":
			slices.SortStableFunc(entries[i:j], func(a, b domain.RadioEntry) int { return domain.CompareChannels(a.Channel, b.Channel) })
		case "freq":
			slices.SortStableFunc(entries[i:j], func(a, b domain.RadioEntry) int { return cmp.Compare(a.Channel.ReceiveHz, b.Channel.ReceiveHz) })
		}
		i = j
	}
	for i := range entries {
		entries[i].Position = i
	}
	return entries, nil
}

// canHold checks a channel can go on the library's radios: one of its own
// (not an annotation), or a shared channel, which the library then follows.
func (l *library) canHold(ctx context.Context, id string) error {
	var own, shared int
	if err := l.q.QueryRowContext(ctx, `SELECT
		(SELECT count(*) FROM channels WHERE id=? AND library_id=? AND link_id IS NULL),
		(SELECT count(*) FROM channels WHERE id=? AND link_id IS NULL AND library_id IN (SELECT id FROM libraries WHERE kind='`+sharedKind+`'))`,
		id, l.id, id).Scan(&own, &shared); err != nil {
		return err
	}
	switch {
	case own > 0:
		return nil
	case shared > 0:
		return l.Follow(ctx, id)
	}
	return fmt.Errorf("channel %q: %w", id, store.ErrNotFound)
}

// renumber stores ids' order as positions 0, 1, 2... Positions order a
// section's channels; the list is in section order first.
func renumber(ctx context.Context, ex execer, radioID string, ids []string) error {
	for i, id := range ids {
		if _, err := ex.ExecContext(ctx, `UPDATE radio_channels SET position=? WHERE radio_id=? AND channel_id=?`, i, radioID, id); err != nil {
			return err
		}
	}
	return nil
}

// entryIDs returns a radio's channel IDs and their sections in list order.
func (l *library) entryIDs(ctx context.Context, radioID string) (ids, sections []string, err error) {
	es, err := l.RadioEntries(ctx, radioID)
	if err != nil {
		return nil, nil, err
	}
	for _, e := range es {
		ids, sections = append(ids, e.Channel.ID), append(sections, e.Section.ID)
	}
	return ids, sections, nil
}

func (l *library) AddRadioChannels(ctx context.Context, radioID, sectionID string, channelIDs []string) (int, error) {
	return l.insertRadioChannels(ctx, radioID, sectionID, "", channelIDs)
}

func (l *library) AddSourceChannels(ctx context.Context, src domain.RadioSource, channelIDs []string) (int, error) {
	return l.insertRadioChannels(ctx, src.RadioID, src.SectionID, src.ID, channelIDs)
}

func (l *library) insertRadioChannels(ctx context.Context, radioID, sectionID, sourceID string, channelIDs []string) (int, error) {
	added := 0
	err := l.inTx(ctx, func(tx *library) error {
		if err := tx.ownsRadio(ctx, radioID); err != nil {
			return err
		}
		section, err := tx.sectionOf(ctx, radioID, sectionID)
		if err != nil {
			return err
		}
		have, err := tx.RadioChannelIDs(ctx, radioID)
		if err != nil {
			return err
		}
		var next int
		if err := tx.q.QueryRowContext(ctx, `SELECT coalesce(max(position),-1)+1 FROM radio_channels WHERE radio_id=?`, radioID).Scan(&next); err != nil {
			return err
		}
		for _, id := range channelIDs {
			if have[id] {
				continue
			}
			if err := tx.canHold(ctx, id); err != nil {
				return err
			}
			if _, err := tx.q.ExecContext(ctx, `INSERT INTO radio_channels(radio_id,channel_id,position,section_id,source_id) VALUES(?,?,?,?,?)`,
				radioID, id, next, section, nullable(sourceID)); err != nil {
				return fmt.Errorf("add channel %q: %w", id, err)
			}
			have[id] = true
			next++
			added++
		}
		return nil
	})
	return added, err
}

// sectionOf checks a section is the radio's; "" is its default section.
func (l *library) sectionOf(ctx context.Context, radioID, sectionID string) (string, error) {
	var id string
	err := l.q.QueryRowContext(ctx, `SELECT id FROM radio_sections WHERE radio_id=? AND (id=? OR (?='' AND is_default=1))`, radioID, sectionID, sectionID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("section %q: %w", sectionID, store.ErrNotFound)
	}
	return id, err
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (l *library) RemoveRadioChannels(ctx context.Context, radioID string, channelIDs []string) (int, error) {
	removed := 0
	err := l.inTx(ctx, func(tx *library) error {
		if err := tx.ownsRadio(ctx, radioID); err != nil {
			return err
		}
		for _, id := range channelIDs {
			res, err := tx.q.ExecContext(ctx, `DELETE FROM radio_channels WHERE radio_id=? AND channel_id=?`, radioID, id)
			if err != nil {
				return err
			}
			n, _ := res.RowsAffected()
			removed += int(n)
		}
		return nil
	})
	return removed, err
}

func (l *library) MoveRadioChannel(ctx context.Context, radioID, channelID, sectionID string, to int) error {
	return l.inTx(ctx, func(tx *library) error {
		ids, sections, err := tx.entryIDs(ctx, radioID)
		if err != nil {
			return err
		}
		from := slices.Index(ids, channelID)
		if from < 0 {
			return fmt.Errorf("channel %q is not on this radio: %w", channelID, store.ErrNotFound)
		}
		target := sections[from]
		if sectionID != "" {
			if target, err = tx.sectionOf(ctx, radioID, sectionID); err != nil {
				return err
			}
		}
		ids = slices.Delete(ids, from, from+1)
		sections = slices.Delete(sections, from, from+1)
		// Where the section's channels are now, and the to'th of them.
		at, n := -1, 0
		for i, sec := range sections {
			if sec != target {
				continue
			}
			if n == to {
				at = i
				break
			}
			n++
			at = i + 1
		}
		if at < 0 {
			at = len(ids) // an empty section: its place doesn't matter
		}
		ids = slices.Insert(ids, at, channelID)
		if _, err := tx.q.ExecContext(ctx, `UPDATE radio_channels SET section_id=? WHERE radio_id=? AND channel_id=?`, target, radioID, channelID); err != nil {
			return err
		}
		return renumber(ctx, tx.q, radioID, ids)
	})
}

func (l *library) ReorderRadioChannels(ctx context.Context, radioID string, channelIDs []string) error {
	return l.inTx(ctx, func(tx *library) error {
		ids, _, err := tx.entryIDs(ctx, radioID)
		if err != nil {
			return err
		}
		if !slices.Equal(slices.Sorted(slices.Values(ids)), slices.Sorted(slices.Values(channelIDs))) {
			return domain.Invalidf("a new order must list the radio's channels exactly once each")
		}
		return renumber(ctx, tx.q, radioID, channelIDs)
	})
}

func (l *library) ReleaseRadioChannels(ctx context.Context, radioID string, channelIDs []string) (int, error) {
	if err := l.ownsRadio(ctx, radioID); err != nil {
		return 0, err
	}
	n := 0
	for _, id := range channelIDs {
		res, err := l.q.ExecContext(ctx, `UPDATE radio_channels SET source_id=NULL, plan_number=NULL WHERE radio_id=? AND channel_id=? AND source_id IS NOT NULL`, radioID, id)
		if err != nil {
			return n, err
		}
		k, _ := res.RowsAffected()
		n += int(k)
	}
	return n, nil
}

func (l *library) SetRadioEntry(ctx context.Context, radioID string, e domain.RadioEntry) error {
	if err := l.ownsRadio(ctx, radioID); err != nil {
		return err
	}
	if _, err := domain.ParsePowerIntent(string(e.Power)); err != nil {
		return err
	}
	var skip any
	if e.Skip != nil {
		skip = *e.Skip
	}
	res, err := l.q.ExecContext(ctx, `UPDATE radio_channels SET name=?,power=?,skip=?,plan_number=? WHERE radio_id=? AND channel_id=?`,
		strings.TrimSpace(e.Name), string(e.Power), skip, intOrNil(e.PlanNumber), radioID, e.Channel.ID)
	if err != nil {
		return fmt.Errorf("update radio channel: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("channel %q is not on this radio: %w", e.Channel.ID, store.ErrNotFound)
	}
	return nil
}

// --- sections ---

const sectionColumns = `id,radio_id,position,name,start,size,sort,is_default`

func listSections(ctx context.Context, q querier, radioID string) ([]domain.RadioSection, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+sectionColumns+` FROM radio_sections WHERE radio_id=? ORDER BY position`, radioID)
	if err != nil {
		return nil, fmt.Errorf("list radio sections: %w", err)
	}
	defer rows.Close()
	var out []domain.RadioSection
	for rows.Next() {
		var sec domain.RadioSection
		var start, size sql.NullInt64
		if err := rows.Scan(&sec.ID, &sec.RadioID, &sec.Position, &sec.Name, &start, &size, &sec.Sort, &sec.Default); err != nil {
			return nil, err
		}
		sec.Start, sec.Size = optionalInt(start), optionalInt(size)
		out = append(out, sec)
	}
	return out, rows.Err()
}

func optionalInt(v sql.NullInt64) *int {
	if !v.Valid {
		return nil
	}
	n := int(v.Int64)
	return &n
}

func (l *library) RadioSections(ctx context.Context, radioID string) ([]domain.RadioSection, error) {
	if err := l.ownsRadio(ctx, radioID); err != nil {
		return nil, err
	}
	return listSections(ctx, l.q, radioID)
}

func (l *library) CreateRadioSection(ctx context.Context, sec domain.RadioSection) (domain.RadioSection, error) {
	if err := domain.ValidateRadioSection(sec); err != nil {
		return sec, err
	}
	err := l.inTx(ctx, func(tx *library) error {
		if err := tx.ownsRadio(ctx, sec.RadioID); err != nil {
			return err
		}
		sections, err := listSections(ctx, tx.q, sec.RadioID)
		if err != nil {
			return err
		}
		var unused time.Time
		if err := assignIdentity(&sec.ID, &unused, &unused); err != nil {
			return err
		}
		sec.Position = len(sections)
		if n := len(sections); n > 0 && sections[n-1].Default {
			sec.Position = n - 1
		}
		if sec.Default {
			if _, err := tx.q.ExecContext(ctx, `UPDATE radio_sections SET is_default=0 WHERE radio_id=?`, sec.RadioID); err != nil {
				return err
			}
		}
		if _, err := tx.q.ExecContext(ctx, `INSERT INTO radio_sections (`+sectionColumns+`) VALUES (?,?,?,?,?,?,?,?)`,
			sec.ID, sec.RadioID, len(sections), sec.Name, intOrNil(sec.Start), intOrNil(sec.Size), sec.Sort, sec.Default); err != nil {
			return fmt.Errorf("add radio section: %w", err)
		}
		ids := make([]string, 0, len(sections)+1)
		for _, x := range sections {
			ids = append(ids, x.ID)
		}
		return renumberSections(ctx, tx.q, slices.Insert(ids, sec.Position, sec.ID))
	})
	return sec, err
}

func renumberSections(ctx context.Context, ex execer, ids []string) error {
	for i, id := range ids {
		if _, err := ex.ExecContext(ctx, `UPDATE radio_sections SET position=? WHERE id=?`, i, id); err != nil {
			return err
		}
	}
	return nil
}

// section returns one of a radio's sections, and all of them.
func (l *library) section(ctx context.Context, radioID, sectionID string) (domain.RadioSection, []domain.RadioSection, error) {
	if err := l.ownsRadio(ctx, radioID); err != nil {
		return domain.RadioSection{}, nil, err
	}
	sections, err := listSections(ctx, l.q, radioID)
	if err != nil {
		return domain.RadioSection{}, nil, err
	}
	i := slices.IndexFunc(sections, func(x domain.RadioSection) bool { return x.ID == sectionID })
	if i < 0 {
		return domain.RadioSection{}, nil, fmt.Errorf("section %q: %w", sectionID, store.ErrNotFound)
	}
	return sections[i], sections, nil
}

func (l *library) UpdateRadioSection(ctx context.Context, sec domain.RadioSection) error {
	if err := domain.ValidateRadioSection(sec); err != nil {
		return err
	}
	return l.inTx(ctx, func(tx *library) error {
		old, _, err := tx.section(ctx, sec.RadioID, sec.ID)
		if err != nil {
			return err
		}
		if old.Default && !sec.Default {
			return domain.Invalidf("%q is the default section; make another section the default instead", old.Name)
		}
		if sec.Default && !old.Default {
			if _, err := tx.q.ExecContext(ctx, `UPDATE radio_sections SET is_default=0 WHERE radio_id=?`, sec.RadioID); err != nil {
				return err
			}
		}
		_, err = tx.q.ExecContext(ctx, `UPDATE radio_sections SET name=?,start=?,size=?,sort=?,is_default=? WHERE id=?`,
			sec.Name, intOrNil(sec.Start), intOrNil(sec.Size), sec.Sort, sec.Default, sec.ID)
		return err
	})
}

func (l *library) MoveRadioSection(ctx context.Context, radioID, sectionID string, to int) error {
	return l.inTx(ctx, func(tx *library) error {
		_, sections, err := tx.section(ctx, radioID, sectionID)
		if err != nil {
			return err
		}
		var ids []string
		for _, x := range sections {
			if x.ID != sectionID {
				ids = append(ids, x.ID)
			}
		}
		to = max(0, min(to, len(ids)))
		return renumberSections(ctx, tx.q, slices.Insert(ids, to, sectionID))
	})
}

func (l *library) DeleteRadioSection(ctx context.Context, radioID, sectionID, intoID string) error {
	return l.inTx(ctx, func(tx *library) error {
		sec, sections, err := tx.section(ctx, radioID, sectionID)
		if err != nil {
			return err
		}
		if intoID == sectionID {
			return domain.Invalidf("merge %q into another section", sec.Name)
		}
		if sec.Default && intoID == "" {
			return domain.Invalidf("%q is the default section; make another section the default before deleting it, or merge it into another", sec.Name)
		}
		def, err := tx.sectionOf(ctx, radioID, intoID)
		if err != nil {
			return err
		}
		if sec.Default {
			// The section it goes into takes over as the default.
			if _, err := tx.q.ExecContext(ctx, `UPDATE radio_sections SET is_default=(id=?) WHERE radio_id=?`, def, radioID); err != nil {
				return err
			}
		}
		// Its channels go to the end of the other section, in order.
		var next int
		if err := tx.q.QueryRowContext(ctx, `SELECT coalesce(max(position),-1)+1 FROM radio_channels WHERE radio_id=?`, radioID).Scan(&next); err != nil {
			return err
		}
		ids, secs, err := tx.entryIDs(ctx, radioID)
		if err != nil {
			return err
		}
		for i, id := range ids {
			if secs[i] != sectionID {
				continue
			}
			if _, err := tx.q.ExecContext(ctx, `UPDATE radio_channels SET section_id=?, position=? WHERE radio_id=? AND channel_id=?`, def, next, radioID, id); err != nil {
				return err
			}
			next++
		}
		if _, err := tx.q.ExecContext(ctx, `UPDATE radio_sources SET section_id=? WHERE section_id=?`, def, sectionID); err != nil {
			return err
		}
		if _, err := tx.q.ExecContext(ctx, `DELETE FROM radio_sections WHERE id=?`, sectionID); err != nil {
			return err
		}
		var rest []string
		for _, x := range sections {
			if x.ID != sectionID {
				rest = append(rest, x.ID)
			}
		}
		return renumberSections(ctx, tx.q, rest)
	})
}

// --- sources ---

const sourceColumns = `id,radio_id,position,query,sort,plan_id,section_id,plan_numbers`

// sourceSelect reads sourceColumns, the plan's name, and the section's.
const sourceSelect = sourceColumns + `,coalesce((SELECT name FROM plans WHERE plans.id=radio_sources.plan_id),''),
	coalesce((SELECT name FROM radio_sections WHERE radio_sections.id=radio_sources.section_id),'')`

func listRadioSources(ctx context.Context, q querier, radioID string) ([]domain.RadioSource, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+sourceSelect+` FROM radio_sources WHERE radio_id=? ORDER BY position`, radioID)
	if err != nil {
		return nil, fmt.Errorf("list radio sources: %w", err)
	}
	return scanSources(rows)
}

func (l *library) RadioSources(ctx context.Context, radioID string) ([]domain.RadioSource, error) {
	if err := l.ownsRadio(ctx, radioID); err != nil {
		return nil, err
	}
	return listRadioSources(ctx, l.q, radioID)
}

func (l *library) AddRadioSource(ctx context.Context, src domain.RadioSource) (domain.RadioSource, error) {
	if err := domain.ValidateRadioSource(src); err != nil {
		return src, err
	}
	err := l.inTx(ctx, func(tx *library) error {
		if err := tx.ownsRadio(ctx, src.RadioID); err != nil {
			return err
		}
		var unused time.Time
		if err := assignIdentity(&src.ID, &unused, &unused); err != nil {
			return err
		}
		if src.SectionID == "" {
			name := src.Query
			if src.PlanID != "" {
				var plan string
				if err := tx.q.QueryRowContext(ctx, `SELECT coalesce((SELECT name FROM plans WHERE id=?),'')`, src.PlanID).Scan(&plan); err != nil {
					return err
				}
				name = "Plan: " + plan
			}
			if len(name) > 40 {
				name = name[:39] + "…"
			}
			sec, err := tx.CreateRadioSection(ctx, domain.RadioSection{RadioID: src.RadioID, Name: name})
			if err != nil {
				return err
			}
			src.SectionID = sec.ID
		} else if _, err := tx.sectionOf(ctx, src.RadioID, src.SectionID); err != nil {
			return err
		}
		existing, err := listRadioSources(ctx, tx.q, src.RadioID)
		if err != nil {
			return err
		}
		src.Position = len(existing)
		_, err = tx.q.ExecContext(ctx, `INSERT INTO radio_sources (`+sourceColumns+`) VALUES (?,?,?,?,?,?,?,?)`,
			src.ID, src.RadioID, src.Position, src.Query, src.Sort, nullable(src.PlanID), src.SectionID, cmp.Or(src.PlanNumbers, domain.PlanNumbersCompact))
		if err != nil {
			return fmt.Errorf("add radio source: %w", err)
		}
		return nil
	})
	return src, err
}

func (l *library) UpdateRadioSource(ctx context.Context, src domain.RadioSource) error {
	if err := domain.ValidateRadioSource(src); err != nil {
		return err
	}
	return l.inTx(ctx, func(tx *library) error {
		if err := tx.ownsRadio(ctx, src.RadioID); err != nil {
			return err
		}
		section, err := tx.sectionOf(ctx, src.RadioID, src.SectionID)
		if err != nil {
			return err
		}
		res, err := tx.q.ExecContext(ctx, `UPDATE radio_sources SET query=?,sort=?,section_id=?,plan_numbers=? WHERE id=? AND radio_id=?`,
			src.Query, src.Sort, section, cmp.Or(src.PlanNumbers, domain.PlanNumbersCompact), src.ID, src.RadioID)
		if err != nil {
			return fmt.Errorf("update radio source: %w", err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return fmt.Errorf("radio source %q: %w", src.ID, store.ErrNotFound)
		}
		return nil
	})
}

func (l *library) RemoveRadioSource(ctx context.Context, src domain.RadioSource, withChannels bool) (int, error) {
	removed := 0
	err := l.inTx(ctx, func(tx *library) error {
		if err := tx.ownsRadio(ctx, src.RadioID); err != nil {
			return err
		}
		if withChannels {
			res, err := tx.q.ExecContext(ctx, `DELETE FROM radio_channels WHERE radio_id=? AND source_id=?`, src.RadioID, src.ID)
			if err != nil {
				return err
			}
			n, _ := res.RowsAffected()
			removed = int(n)
		}
		if _, err := tx.q.ExecContext(ctx, `UPDATE radio_channels SET plan_number=NULL WHERE radio_id=? AND source_id=?`, src.RadioID, src.ID); err != nil {
			return err
		}
		res, err := tx.q.ExecContext(ctx, `DELETE FROM radio_sources WHERE id=? AND radio_id=?`, src.ID, src.RadioID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return fmt.Errorf("radio source %q: %w", src.ID, store.ErrNotFound)
		}
		sources, err := listRadioSources(ctx, tx.q, src.RadioID)
		if err != nil {
			return err
		}
		return renumberSources(ctx, tx.q, sources)
	})
	return removed, err
}

func (l *library) MoveRadioSource(ctx context.Context, src domain.RadioSource, to int) error {
	return l.inTx(ctx, func(tx *library) error {
		if err := tx.ownsRadio(ctx, src.RadioID); err != nil {
			return err
		}
		sources, err := listRadioSources(ctx, tx.q, src.RadioID)
		if err != nil {
			return err
		}
		from := slices.IndexFunc(sources, func(x domain.RadioSource) bool { return x.ID == src.ID })
		if from < 0 {
			return fmt.Errorf("radio source %q: %w", src.ID, store.ErrNotFound)
		}
		moving := sources[from]
		sources = slices.Delete(sources, from, from+1)
		to = max(0, min(to, len(sources)))
		sources = slices.Insert(sources, to, moving)
		return renumberSources(ctx, tx.q, sources)
	})
}

func renumberSources(ctx context.Context, ex execer, sources []domain.RadioSource) error {
	for i, src := range sources {
		if _, err := ex.ExecContext(ctx, `UPDATE radio_sources SET position=? WHERE id=?`, i, src.ID); err != nil {
			return err
		}
	}
	return nil
}

func (l *library) AllRadioSources(ctx context.Context) ([]domain.RadioSource, error) {
	rows, err := l.q.QueryContext(ctx, `SELECT `+sourceSelect+` FROM radio_sources WHERE radio_id IN (SELECT id FROM radios WHERE library_id=?) ORDER BY radio_id, position`, l.id)
	if err != nil {
		return nil, err
	}
	return scanSources(rows)
}

func scanSources(rows *sql.Rows) ([]domain.RadioSource, error) {
	defer rows.Close()
	var out []domain.RadioSource
	for rows.Next() {
		var src domain.RadioSource
		var plan, section sql.NullString
		if err := rows.Scan(&src.ID, &src.RadioID, &src.Position, &src.Query, &src.Sort, &plan, &section, &src.PlanNumbers, &src.PlanName, &src.SectionName); err != nil {
			return nil, err
		}
		src.PlanID, src.SectionID = plan.String, section.String
		out = append(out, src)
	}
	return out, rows.Err()
}

func intOrNil(v *int) any {
	if v == nil {
		return nil
	}
	return *v
}

// ReplaceOnRadios puts newID in oldID's place on every radio of the library
// that holds oldID, keeping its section and overrides; a radio that holds
// newID already just drops oldID.
func (l *library) ReplaceOnRadios(ctx context.Context, oldID, newID string) error {
	return l.inTx(ctx, func(tx *library) error {
		if err := tx.canHold(ctx, newID); err != nil {
			return err
		}
		rows, err := tx.q.QueryContext(ctx, `SELECT radio_id FROM radio_channels WHERE channel_id=? AND radio_id IN (SELECT id FROM radios WHERE library_id=?)`, oldID, tx.id)
		if err != nil {
			return err
		}
		var radios []string
		for rows.Next() {
			var r string
			if err := rows.Scan(&r); err != nil {
				_ = rows.Close()
				return err
			}
			radios = append(radios, r)
		}
		if err := rows.Close(); err != nil {
			return err
		}
		for _, r := range radios {
			var has int
			if err := tx.q.QueryRowContext(ctx, `SELECT count(*) FROM radio_channels WHERE radio_id=? AND channel_id=?`, r, newID).Scan(&has); err != nil {
				return err
			}
			if has == 0 {
				if _, err := tx.q.ExecContext(ctx, `UPDATE radio_channels SET channel_id=? WHERE radio_id=? AND channel_id=?`, newID, r, oldID); err != nil {
					return err
				}
				continue
			}
			if _, err := tx.q.ExecContext(ctx, `DELETE FROM radio_channels WHERE radio_id=? AND channel_id=?`, r, oldID); err != nil {
				return err
			}
		}
		return nil
	})
}
