package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ryan/rflib/internal/domain"
)

const radioColumns = `id,name,model,default_power,dedupe,notes,created_at,updated_at`

func (s *Store) CreateRadio(ctx context.Context, r domain.Radio) (domain.Radio, error) {
	if err := domain.ValidateRadio(r); err != nil {
		return r, err
	}
	if err := assignIdentity(&r.ID, &r.CreatedAt, &r.UpdatedAt); err != nil {
		return r, err
	}
	_, err := s.q.ExecContext(ctx, `INSERT INTO radios (`+radioColumns+`) VALUES (?,?,?,?,?,?,?,?)`,
		r.ID, r.Name, r.Model, string(r.DefaultPower), r.Dedupe, r.Notes,
		r.CreatedAt.Format(time.RFC3339Nano), r.UpdatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return r, fmt.Errorf("create radio: %w", err)
	}
	return r, nil
}

// GetRadio looks a radio up by ID or by its unique name.
func (s *Store) GetRadio(ctx context.Context, idOrName string) (domain.Radio, error) {
	r, err := scanRadio(s.q.QueryRowContext(ctx, `SELECT `+radioColumns+` FROM radios WHERE id=? OR name=?`, idOrName, idOrName))
	if errors.Is(err, sql.ErrNoRows) {
		return r, fmt.Errorf("radio %q: %w", idOrName, ErrNotFound)
	}
	return r, err
}

func (s *Store) ListRadios(ctx context.Context) ([]domain.Radio, error) {
	rows, err := s.q.QueryContext(ctx, `SELECT `+radioColumns+` FROM radios ORDER BY name`)
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

func (s *Store) UpdateRadio(ctx context.Context, r domain.Radio) (domain.Radio, error) {
	if err := domain.ValidateRadio(r); err != nil {
		return r, err
	}
	r.UpdatedAt = time.Now().UTC()
	res, err := s.q.ExecContext(ctx, `UPDATE radios SET name=?,model=?,default_power=?,dedupe=?,notes=?,updated_at=? WHERE id=?`,
		r.Name, r.Model, string(r.DefaultPower), r.Dedupe, r.Notes, r.UpdatedAt.Format(time.RFC3339Nano), r.ID)
	if err != nil {
		return r, fmt.Errorf("update radio: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return r, fmt.Errorf("radio %q: %w", r.ID, ErrNotFound)
	}
	return r, nil
}

// DeleteRadio deletes a radio by ID or name. Its channels are untouched.
func (s *Store) DeleteRadio(ctx context.Context, idOrName string) error {
	res, err := s.q.ExecContext(ctx, `DELETE FROM radios WHERE id=? OR name=?`, idOrName, idOrName)
	if err != nil {
		return fmt.Errorf("delete radio: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("radio %q: %w", idOrName, ErrNotFound)
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
	x.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	x.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return x, nil
}

// RadioEntries returns a radio's channels in memory order, with the radio's
// overrides for each.
func (s *Store) RadioEntries(ctx context.Context, radioID string) ([]domain.RadioEntry, error) {
	rows, err := s.q.QueryContext(ctx, `SELECT channel_id,section_start,name,power,skip,source_id FROM radio_channels WHERE radio_id=? ORDER BY position`, radioID)
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
	channels, err := s.listChannels(ctx, ` WHERE c.id IN (SELECT channel_id FROM radio_channels WHERE radio_id=?)`, []any{radioID})
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

// AddRadioChannels appends channels to the end of a radio's list, in the
// order given, skipping any already on it. It returns how many were added.
func (s *Store) AddRadioChannels(ctx context.Context, radioID string, channelIDs []string) (int, error) {
	return s.insertRadioChannels(ctx, radioID, nil, channelIDs)
}

// AddSourceChannels adds channels for a radio source, skipping any already
// on the radio. They go after the last channel the source added; for a
// source with none yet, after the channels of earlier sources and before
// those of later ones, else at the end. The source's section start, when
// set, is kept on its first channel.
func (s *Store) AddSourceChannels(ctx context.Context, src domain.RadioSource, channelIDs []string) (int, error) {
	return s.insertRadioChannels(ctx, src.RadioID, &src, channelIDs)
}

type orderRow struct {
	channelID, sourceID string
}

func (s *Store) insertRadioChannels(ctx context.Context, radioID string, src *domain.RadioSource, channelIDs []string) (int, error) {
	added := 0
	err := s.InTx(ctx, func(tx *Store) error {
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

// RemoveRadioChannels takes channels off a radio and closes up the list.
// It returns how many were removed.
func (s *Store) RemoveRadioChannels(ctx context.Context, radioID string, channelIDs []string) (int, error) {
	removed := 0
	err := s.InTx(ctx, func(tx *Store) error {
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

// MoveRadioChannel moves a channel to index to (0-based) in the radio's
// list; an index past the end moves it last.
func (s *Store) MoveRadioChannel(ctx context.Context, radioID, channelID string, to int) error {
	return s.InTx(ctx, func(tx *Store) error {
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
			return fmt.Errorf("channel %q is not on this radio: %w", channelID, ErrNotFound)
		}
		order = append(order[:from], order[from+1:]...)
		to = max(0, min(to, len(order)))
		order = append(order[:to], append([]string{channelID}, order[to:]...)...)
		return renumber(ctx, tx.q, radioID, order)
	})
}

// SetRadioEntry stores a radio's overrides for one of its channels: the
// entry's SectionStart, Name, Power, and Skip.
func (s *Store) SetRadioEntry(ctx context.Context, radioID string, e domain.RadioEntry) error {
	if _, err := domain.ParsePowerIntent(string(e.Power)); err != nil {
		return err
	}
	if e.SectionStart != nil && *e.SectionStart < 0 {
		return fmt.Errorf("section start must not be negative")
	}
	var section, skip any
	if e.SectionStart != nil {
		section = *e.SectionStart
	}
	if e.Skip != nil {
		skip = *e.Skip
	}
	res, err := s.q.ExecContext(ctx, `UPDATE radio_channels SET section_start=?,name=?,power=?,skip=? WHERE radio_id=? AND channel_id=?`,
		section, strings.TrimSpace(e.Name), string(e.Power), skip, radioID, e.Channel.ID)
	if err != nil {
		return fmt.Errorf("update radio channel: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("channel %q is not on this radio: %w", e.Channel.ID, ErrNotFound)
	}
	return nil
}

const sourceColumns = `id,radio_id,position,query,sort,section_start`

func listRadioSources(ctx context.Context, q querier, radioID string) ([]domain.RadioSource, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+sourceColumns+` FROM radio_sources WHERE radio_id=? ORDER BY position`, radioID)
	if err != nil {
		return nil, fmt.Errorf("list radio sources: %w", err)
	}
	return scanSources(rows)
}

// RadioSources returns a radio's sources in order.
func (s *Store) RadioSources(ctx context.Context, radioID string) ([]domain.RadioSource, error) {
	return listRadioSources(ctx, s.q, radioID)
}

// AddRadioSource appends a source to a radio's list.
func (s *Store) AddRadioSource(ctx context.Context, src domain.RadioSource) (domain.RadioSource, error) {
	if err := domain.ValidateRadioSource(src); err != nil {
		return src, err
	}
	err := s.InTx(ctx, func(tx *Store) error {
		var unused time.Time
		if err := assignIdentity(&src.ID, &unused, &unused); err != nil {
			return err
		}
		existing, err := listRadioSources(ctx, tx.q, src.RadioID)
		if err != nil {
			return err
		}
		src.Position = len(existing)
		_, err = tx.q.ExecContext(ctx, `INSERT INTO radio_sources (`+sourceColumns+`) VALUES (?,?,?,?,?,?)`,
			src.ID, src.RadioID, src.Position, src.Query, src.Sort, intOrNil(src.SectionStart))
		if err != nil {
			return fmt.Errorf("add radio source: %w", err)
		}
		return nil
	})
	return src, err
}

// UpdateRadioSource changes a source's query, sort, and section start. A
// changed section start moves to the source's first channel now; a removed
// one is taken off it, if it is still the source's.
func (s *Store) UpdateRadioSource(ctx context.Context, src domain.RadioSource) error {
	if err := domain.ValidateRadioSource(src); err != nil {
		return err
	}
	return s.InTx(ctx, func(tx *Store) error {
		sources, err := listRadioSources(ctx, tx.q, src.RadioID)
		if err != nil {
			return err
		}
		i := slices.IndexFunc(sources, func(x domain.RadioSource) bool { return x.ID == src.ID })
		if i < 0 {
			return fmt.Errorf("radio source %q: %w", src.ID, ErrNotFound)
		}
		old := sources[i]
		if _, err := tx.q.ExecContext(ctx, `UPDATE radio_sources SET query=?,sort=?,section_start=? WHERE id=?`,
			src.Query, src.Sort, intOrNil(src.SectionStart), src.ID); err != nil {
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

// RemoveRadioSource deletes a source. Its channels stay on the radio as if
// added by hand, unless withChannels, which takes them off too. It returns
// how many channels were taken off.
func (s *Store) RemoveRadioSource(ctx context.Context, src domain.RadioSource, withChannels bool) (int, error) {
	removed := 0
	err := s.InTx(ctx, func(tx *Store) error {
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
			return fmt.Errorf("radio source %q: %w", src.ID, ErrNotFound)
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

// MoveRadioSource moves a source to index to (0-based) in its radio's
// list. Channels already on the radio stay where they are.
func (s *Store) MoveRadioSource(ctx context.Context, src domain.RadioSource, to int) error {
	return s.InTx(ctx, func(tx *Store) error {
		sources, err := listRadioSources(ctx, tx.q, src.RadioID)
		if err != nil {
			return err
		}
		from := slices.IndexFunc(sources, func(x domain.RadioSource) bool { return x.ID == src.ID })
		if from < 0 {
			return fmt.Errorf("radio source %q: %w", src.ID, ErrNotFound)
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

// AllRadioSources returns every radio's sources.
func (s *Store) AllRadioSources(ctx context.Context) ([]domain.RadioSource, error) {
	rows, err := s.q.QueryContext(ctx, `SELECT `+sourceColumns+` FROM radio_sources ORDER BY radio_id, position`)
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
		if err := rows.Scan(&src.ID, &src.RadioID, &src.Position, &src.Query, &src.Sort, &section); err != nil {
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
