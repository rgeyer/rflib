package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ryan/rflib/internal/domain"
)

const radioColumns = `id,name,model,default_power,dedupe,sync_tag,sync_group_id,sync_service,notes,created_at,updated_at`

func (s *Store) CreateRadio(ctx context.Context, r domain.Radio) (domain.Radio, error) {
	if err := domain.ValidateRadio(r); err != nil {
		return r, err
	}
	if err := assignIdentity(&r.ID, &r.CreatedAt, &r.UpdatedAt); err != nil {
		return r, err
	}
	_, err := s.q.ExecContext(ctx, `INSERT INTO radios (`+radioColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		r.ID, r.Name, r.Model, string(r.DefaultPower), r.Dedupe, r.Sync.Tag, nullable(r.Sync.GroupID), string(r.Sync.Service), r.Notes,
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
	res, err := s.q.ExecContext(ctx, `UPDATE radios SET name=?,model=?,default_power=?,dedupe=?,sync_tag=?,sync_group_id=?,sync_service=?,notes=?,updated_at=? WHERE id=?`,
		r.Name, r.Model, string(r.DefaultPower), r.Dedupe, r.Sync.Tag, nullable(r.Sync.GroupID), string(r.Sync.Service), r.Notes, r.UpdatedAt.Format(time.RFC3339Nano), r.ID)
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
	var group sql.NullString
	var created, updated string
	err := r.Scan(&x.ID, &x.Name, &x.Model, &x.DefaultPower, &x.Dedupe, &x.Sync.Tag, &group, &x.Sync.Service, &x.Notes, &created, &updated)
	if err != nil {
		return x, err
	}
	x.Sync.GroupID = group.String
	x.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	x.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return x, nil
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// RadioEntries returns a radio's channels in memory order, with the radio's
// overrides for each.
func (s *Store) RadioEntries(ctx context.Context, radioID string) ([]domain.RadioEntry, error) {
	rows, err := s.q.QueryContext(ctx, `SELECT channel_id,section_start,name,power,skip FROM radio_channels WHERE radio_id=? ORDER BY position`, radioID)
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
		if err := rows.Scan(&id, &section, &e.Name, &e.Power, &skip); err != nil {
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
	added := 0
	err := s.InTx(ctx, func(tx *Store) error {
		order, err := radioOrder(ctx, tx.q, radioID)
		if err != nil {
			return err
		}
		have := make(map[string]bool, len(order))
		for _, id := range order {
			have[id] = true
		}
		next := len(order)
		for _, id := range channelIDs {
			if have[id] {
				continue
			}
			if _, err := tx.q.ExecContext(ctx, `INSERT INTO radio_channels(radio_id,channel_id,position) VALUES(?,?,?)`, radioID, id, next); err != nil {
				return fmt.Errorf("add channel %q: %w", id, err)
			}
			have[id] = true
			next++
			added++
		}
		// Deleted channels leave gaps; close them while we're here.
		order, err = radioOrder(ctx, tx.q, radioID)
		if err != nil {
			return err
		}
		return renumber(ctx, tx.q, radioID, order)
	})
	return added, err
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
