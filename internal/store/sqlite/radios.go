package sqlite

import (
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

const radioColumns = `id,name,model,default_power,dedupe,notes,created_at,updated_at`

func (l *library) CreateRadio(ctx context.Context, r domain.Radio) (domain.Radio, error) {
	if err := domain.ValidateRadio(r); err != nil {
		return r, err
	}
	if err := assignIdentity(&r.ID, &r.CreatedAt, &r.UpdatedAt); err != nil {
		return r, err
	}
	_, err := l.q.ExecContext(ctx, `INSERT INTO radios (library_id,`+radioColumns+`) VALUES (?,?,?,?,?,?,?,?,?)`,
		l.id, r.ID, r.Name, r.Model, string(r.DefaultPower), r.Dedupe, r.Notes, timestamp(r.CreatedAt), timestamp(r.UpdatedAt))
	if err != nil {
		return r, conflict(fmt.Errorf("create radio: %w", err), "a radio named %q", r.Name)
	}
	return r, nil
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
	res, err := l.q.ExecContext(ctx, `UPDATE radios SET name=?,model=?,default_power=?,dedupe=?,notes=?,updated_at=? WHERE id=? AND library_id=?`,
		r.Name, r.Model, string(r.DefaultPower), r.Dedupe, r.Notes, timestamp(r.UpdatedAt), r.ID, l.id)
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
	err := r.Scan(&x.ID, &x.Name, &x.Model, &x.DefaultPower, &x.Dedupe, &x.Notes, &created, &updated)
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
	rows, err := l.q.QueryContext(ctx, `SELECT channel_id,section_start,name,power,skip,source_id FROM radio_channels WHERE radio_id=? ORDER BY position`, radioID)
	if err != nil {
		return nil, fmt.Errorf("list radio channels: %w", err)
	}
	var entries []domain.RadioEntry
	var ids []string
	for rows.Next() {
		var e domain.RadioEntry
		var id string
		var section sql.NullInt64
		var skip sql.NullBool
		var source sql.NullString
		if err := rows.Scan(&id, &section, &e.Name, &e.Power, &skip, &source); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if section.Valid {
			v := int(section.Int64)
			e.SectionStart = &v
		}
		if skip.Valid {
			e.Skip = &skip.Bool
		}
		e.SourceID = source.String
		e.Position = len(entries)
		entries = append(entries, e)
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	channels, err := l.listChannels(ctx, `c.library_id=? AND c.id IN (SELECT channel_id FROM radio_channels WHERE radio_id=?)`, l.id, radioID)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]domain.Channel, len(channels))
	for _, c := range channels {
		byID[c.ID] = c
	}
	for i, id := range ids {
		entries[i].Channel = byID[id]
	}
	return entries, nil
}

// radioOrder returns a radio's channel IDs in memory order.
func radioOrder(ctx context.Context, q querier, radioID string) ([]string, error) {
	rows, err := q.QueryContext(ctx, `SELECT channel_id FROM radio_channels WHERE radio_id=? ORDER BY position`, radioID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// renumber stores ids' order as positions 0, 1, 2...
func renumber(ctx context.Context, ex execer, radioID string, ids []string) error {
	for i, id := range ids {
		if _, err := ex.ExecContext(ctx, `UPDATE radio_channels SET position=? WHERE radio_id=? AND channel_id=?`, i, radioID, id); err != nil {
			return err
		}
	}
	return nil
}

func (l *library) AddRadioChannels(ctx context.Context, radioID string, channelIDs []string) (int, error) {
	return l.insertRadioChannels(ctx, radioID, nil, channelIDs)
}

func (l *library) AddSourceChannels(ctx context.Context, src domain.RadioSource, channelIDs []string) (int, error) {
	return l.insertRadioChannels(ctx, src.RadioID, &src, channelIDs)
}

type orderRow struct {
	channelID, sourceID string
}

func (l *library) insertRadioChannels(ctx context.Context, radioID string, src *domain.RadioSource, channelIDs []string) (int, error) {
	added := 0
	err := l.inTx(ctx, func(tx *library) error {
		if err := tx.ownsRadio(ctx, radioID); err != nil {
			return err
		}
		rows, err := radioRows(ctx, tx.q, radioID)
		if err != nil {
			return err
		}
		have := make(map[string]bool, len(rows))
		for _, r := range rows {
			have[r.channelID] = true
		}
		var add []orderRow
		for _, id := range channelIDs {
			if !have[id] {
				if err := tx.owns(ctx, "channels", "channel", id); err != nil {
					return err
				}
				have[id] = true
				add = append(add, orderRow{channelID: id})
			}
		}
		at := len(rows)
		sourceID := ""
		if src != nil {
			sourceID = src.ID
			if at, err = sourceInsertAt(ctx, tx.q, *src, rows); err != nil {
				return err
			}
			for i := range add {
				add[i].sourceID = src.ID
			}
		}
		for i, r := range add {
			if _, err := tx.q.ExecContext(ctx, `INSERT INTO radio_channels(radio_id,channel_id,position,source_id) VALUES(?,?,?,?)`,
				radioID, r.channelID, at+i, nullable(sourceID)); err != nil {
				return fmt.Errorf("add channel %q: %w", r.channelID, err)
			}
		}
		added = len(add)
		order := make([]string, 0, len(rows)+len(add))
		for _, r := range rows[:at] {
			order = append(order, r.channelID)
		}
		for _, r := range add {
			order = append(order, r.channelID)
		}
		for _, r := range rows[at:] {
			order = append(order, r.channelID)
		}
		if err := renumber(ctx, tx.q, radioID, order); err != nil {
			return err
		}
		if src != nil && src.SectionStart != nil {
			return keepSectionStart(ctx, tx.q, *src)
		}
		return nil
	})
	return added, err
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// radioRows returns a radio's channels and their sources in memory order.
func radioRows(ctx context.Context, q querier, radioID string) ([]orderRow, error) {
	rows, err := q.QueryContext(ctx, `SELECT channel_id,coalesce(source_id,'') FROM radio_channels WHERE radio_id=? ORDER BY position`, radioID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []orderRow
	for rows.Next() {
		var r orderRow
		if err := rows.Scan(&r.channelID, &r.sourceID); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// sourceInsertAt is the list index a source's new channels go at; see
// AddSourceChannels.
func sourceInsertAt(ctx context.Context, q querier, src domain.RadioSource, rows []orderRow) (int, error) {
	for i := len(rows) - 1; i >= 0; i-- {
		if rows[i].sourceID == src.ID {
			return i + 1, nil
		}
	}
	sources, err := listRadioSources(ctx, q, src.RadioID)
	if err != nil {
		return 0, err
	}
	pos := map[string]int{}
	for _, x := range sources {
		pos[x.ID] = x.Position
	}
	at := -1
	for i, r := range rows {
		if p, ok := pos[r.sourceID]; ok && r.sourceID != "" && p < src.Position {
			at = i + 1
		}
	}
	if at >= 0 {
		return at, nil
	}
	for i, r := range rows {
		if p, ok := pos[r.sourceID]; ok && r.sourceID != "" && p > src.Position {
			return i, nil
		}
	}
	return len(rows), nil
}

// keepSectionStart puts a source's section start on its first channel.
func keepSectionStart(ctx context.Context, ex execer, src domain.RadioSource) error {
	_, err := ex.ExecContext(ctx, `UPDATE radio_channels SET section_start=? WHERE radio_id=? AND channel_id=
		(SELECT channel_id FROM radio_channels WHERE radio_id=? AND source_id=? ORDER BY position LIMIT 1)`,
		*src.SectionStart, src.RadioID, src.RadioID, src.ID)
	return err
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
		order, err := radioOrder(ctx, tx.q, radioID)
		if err != nil {
			return err
		}
		return renumber(ctx, tx.q, radioID, order)
	})
	return removed, err
}

func (l *library) MoveRadioChannel(ctx context.Context, radioID, channelID string, to int) error {
	return l.inTx(ctx, func(tx *library) error {
		if err := tx.ownsRadio(ctx, radioID); err != nil {
			return err
		}
		order, err := radioOrder(ctx, tx.q, radioID)
		if err != nil {
			return err
		}
		from := -1
		for i, id := range order {
			if id == channelID {
				from = i
			}
		}
		if from < 0 {
			return fmt.Errorf("channel %q is not on this radio: %w", channelID, store.ErrNotFound)
		}
		order = append(order[:from], order[from+1:]...)
		to = max(0, min(to, len(order)))
		order = append(order[:to], append([]string{channelID}, order[to:]...)...)
		// A channel placed by hand is no longer its source's to place.
		if _, err := tx.q.ExecContext(ctx, `UPDATE radio_channels SET source_id=NULL WHERE radio_id=? AND channel_id=?`, radioID, channelID); err != nil {
			return err
		}
		return renumber(ctx, tx.q, radioID, order)
	})
}

func (l *library) ReorderRadioChannels(ctx context.Context, radioID string, channelIDs []string) error {
	return l.inTx(ctx, func(tx *library) error {
		if err := tx.ownsRadio(ctx, radioID); err != nil {
			return err
		}
		order, err := radioOrder(ctx, tx.q, radioID)
		if err != nil {
			return err
		}
		if !slices.Equal(slices.Sorted(slices.Values(order)), slices.Sorted(slices.Values(channelIDs))) {
			return domain.Invalidf("a new order must list the radio's channels exactly once each")
		}
		sections := make([]sql.NullInt64, len(order))
		for i, id := range order {
			if err := tx.q.QueryRowContext(ctx, `SELECT section_start FROM radio_channels WHERE radio_id=? AND channel_id=?`, radioID, id).Scan(&sections[i]); err != nil {
				return err
			}
		}
		for i, id := range channelIDs {
			if _, err := tx.q.ExecContext(ctx, `UPDATE radio_channels SET section_start=? WHERE radio_id=? AND channel_id=?`, sections[i], radioID, id); err != nil {
				return err
			}
		}
		return renumber(ctx, tx.q, radioID, channelIDs)
	})
}

func (l *library) SetRadioEntry(ctx context.Context, radioID string, e domain.RadioEntry) error {
	if err := l.ownsRadio(ctx, radioID); err != nil {
		return err
	}
	if _, err := domain.ParsePowerIntent(string(e.Power)); err != nil {
		return err
	}
	if e.SectionStart != nil && *e.SectionStart < 0 {
		return domain.Invalidf("section start must not be negative")
	}
	var section, skip any
	if e.SectionStart != nil {
		section = *e.SectionStart
	}
	if e.Skip != nil {
		skip = *e.Skip
	}
	res, err := l.q.ExecContext(ctx, `UPDATE radio_channels SET section_start=?,name=?,power=?,skip=? WHERE radio_id=? AND channel_id=?`,
		section, strings.TrimSpace(e.Name), string(e.Power), skip, radioID, e.Channel.ID)
	if err != nil {
		return fmt.Errorf("update radio channel: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("channel %q is not on this radio: %w", e.Channel.ID, store.ErrNotFound)
	}
	return nil
}

const sourceColumns = `id,radio_id,position,query,sort,section_start,keep_sorted`

func listRadioSources(ctx context.Context, q querier, radioID string) ([]domain.RadioSource, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+sourceColumns+` FROM radio_sources WHERE radio_id=? ORDER BY position`, radioID)
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
		existing, err := listRadioSources(ctx, tx.q, src.RadioID)
		if err != nil {
			return err
		}
		src.Position = len(existing)
		_, err = tx.q.ExecContext(ctx, `INSERT INTO radio_sources (`+sourceColumns+`) VALUES (?,?,?,?,?,?,?)`,
			src.ID, src.RadioID, src.Position, src.Query, src.Sort, intOrNil(src.SectionStart), src.KeepSorted)
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
		sources, err := listRadioSources(ctx, tx.q, src.RadioID)
		if err != nil {
			return err
		}
		i := slices.IndexFunc(sources, func(x domain.RadioSource) bool { return x.ID == src.ID })
		if i < 0 {
			return fmt.Errorf("radio source %q: %w", src.ID, store.ErrNotFound)
		}
		old := sources[i]
		if _, err := tx.q.ExecContext(ctx, `UPDATE radio_sources SET query=?,sort=?,section_start=?,keep_sorted=? WHERE id=?`,
			src.Query, src.Sort, intOrNil(src.SectionStart), src.KeepSorted, src.ID); err != nil {
			return fmt.Errorf("update radio source: %w", err)
		}
		switch {
		case src.SectionStart != nil:
			return keepSectionStart(ctx, tx.q, src)
		case old.SectionStart != nil:
			_, err := tx.q.ExecContext(ctx, `UPDATE radio_channels SET section_start=NULL WHERE radio_id=? AND source_id=? AND section_start=?`,
				src.RadioID, src.ID, *old.SectionStart)
			return err
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
		if err := renumberSources(ctx, tx.q, sources); err != nil {
			return err
		}
		order, err := radioOrder(ctx, tx.q, src.RadioID)
		if err != nil {
			return err
		}
		return renumber(ctx, tx.q, src.RadioID, order)
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
	rows, err := l.q.QueryContext(ctx, `SELECT `+sourceColumns+` FROM radio_sources WHERE radio_id IN (SELECT id FROM radios WHERE library_id=?) ORDER BY radio_id, position`, l.id)
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
		var section sql.NullInt64
		if err := rows.Scan(&src.ID, &src.RadioID, &src.Position, &src.Query, &src.Sort, &section, &src.KeepSorted); err != nil {
			return nil, err
		}
		if section.Valid {
			v := int(section.Int64)
			src.SectionStart = &v
		}
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
