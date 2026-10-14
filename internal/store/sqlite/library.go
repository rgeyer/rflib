package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ryan/rflib/internal/domain"
	"github.com/ryan/rflib/internal/store"
)

// library is one library's data. Every statement is confined to its ID. A
// library bound to a transaction (tx set) runs everything in it.
type library struct {
	db *sql.DB
	q  querier
	tx *sql.Tx
	id string
}

var _ store.Library = (*library)(nil)

func (l *library) ID() string { return l.id }

func (l *library) InTx(ctx context.Context, fn func(store.Library) error) error {
	return l.inTx(ctx, func(tx *library) error { return fn(tx) })
}

func (l *library) inTx(ctx context.Context, fn func(*library) error) error {
	if l.tx != nil {
		return fn(l)
	}
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(&library{db: l.db, q: tx, tx: tx, id: l.id}); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// owns checks that a record in table belongs to this library.
func (l *library) owns(ctx context.Context, table, what, id string) error {
	var n int
	if err := l.q.QueryRowContext(ctx, `SELECT count(*) FROM `+table+` WHERE id=? AND library_id=?`, id, l.id).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("%s %q: %w", what, id, store.ErrNotFound)
	}
	return nil
}

// --- channels ---

// prepareChannel normalizes and validates a channel.
func prepareChannel(c *domain.Channel) error {
	c.Mode = strings.ToUpper(strings.TrimSpace(c.Mode))
	if err := domain.ValidateChannel(*c); err != nil {
		return err
	}
	c.Tags = domain.NormalizeTags(c.Tags)
	c.ShortNames = domain.NormalizeShortNames(c.Name, c.ShortNames)
	return nil
}

func (l *library) CreateChannel(ctx context.Context, c domain.Channel) (domain.Channel, error) {
	err := l.inTx(ctx, func(tx *library) error {
		if err := prepareChannel(&c); err != nil {
			return err
		}
		if err := freeID(ctx, tx.q, "channels", &c.ID); err != nil {
			return err
		}
		if err := assignIdentity(&c.ID, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return err
		}
		rt, tt := toneArgs(c.ReceiveTone), toneArgs(c.TransmitTone)
		lat, lon, radius := locationArgs(c.Location)
		if _, err := tx.q.ExecContext(ctx, `INSERT INTO channels (id,library_id,latitude,longitude,radius_km,name,receive_hz,transmit_hz,mode,service,rx_only,receive_tone_kind,receive_ctcss_millihz,receive_dcs_code,receive_dcs_inverted,transmit_tone_kind,transmit_ctcss_millihz,transmit_dcs_code,transmit_dcs_inverted,tone_squelch,power,bandwidth,skip,notes,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			c.ID, tx.id, lat, lon, radius, c.Name, c.ReceiveHz, c.TransmitHz, c.Mode, string(c.Service), c.RXOnly, rt.kind, rt.ctcss, rt.dcs, rt.inv, tt.kind, tt.ctcss, tt.dcs, tt.inv,
			c.ToneSquelch, string(c.Power), c.Bandwidth, c.Skip, c.Notes, timestamp(c.CreatedAt), timestamp(c.UpdatedAt)); err != nil {
			return fmt.Errorf("create channel: %w", err)
		}
		return writeChannelLists(ctx, tx.q, c)
	})
	return c, err
}

func (l *library) UpdateChannel(ctx context.Context, c domain.Channel) (domain.Channel, error) {
	err := l.inTx(ctx, func(tx *library) error {
		if err := prepareChannel(&c); err != nil {
			return err
		}
		c.UpdatedAt = time.Now().UTC()
		rt, tt := toneArgs(c.ReceiveTone), toneArgs(c.TransmitTone)
		lat, lon, radius := locationArgs(c.Location)
		r, err := tx.q.ExecContext(ctx, `UPDATE channels SET latitude=?,longitude=?,radius_km=?,name=?,receive_hz=?,transmit_hz=?,mode=?,service=?,rx_only=?,receive_tone_kind=?,receive_ctcss_millihz=?,receive_dcs_code=?,receive_dcs_inverted=?,transmit_tone_kind=?,transmit_ctcss_millihz=?,transmit_dcs_code=?,transmit_dcs_inverted=?,tone_squelch=?,power=?,bandwidth=?,skip=?,notes=?,updated_at=? WHERE id=? AND library_id=?`,
			lat, lon, radius, c.Name, c.ReceiveHz, c.TransmitHz, c.Mode, string(c.Service), c.RXOnly, rt.kind, rt.ctcss, rt.dcs, rt.inv, tt.kind, tt.ctcss, tt.dcs, tt.inv,
			c.ToneSquelch, string(c.Power), c.Bandwidth, c.Skip, c.Notes, timestamp(c.UpdatedAt), c.ID, tx.id)
		if err != nil {
			return fmt.Errorf("update channel: %w", err)
		}
		if n, _ := r.RowsAffected(); n == 0 {
			return fmt.Errorf("channel %q: %w", c.ID, store.ErrNotFound)
		}
		return writeChannelLists(ctx, tx.q, c)
	})
	return c, err
}

// writeChannelLists replaces a channel's tags, and its short names in the
// user's order.
func writeChannelLists(ctx context.Context, ex execer, c domain.Channel) error {
	if _, err := ex.ExecContext(ctx, `DELETE FROM channel_tags WHERE channel_id=?`, c.ID); err != nil {
		return err
	}
	for _, tag := range c.Tags {
		if _, err := ex.ExecContext(ctx, `INSERT INTO channel_tags(channel_id,tag) VALUES(?,?)`, c.ID, tag); err != nil {
			return err
		}
	}
	if _, err := ex.ExecContext(ctx, `DELETE FROM channel_short_names WHERE channel_id=?`, c.ID); err != nil {
		return err
	}
	for i, name := range c.ShortNames {
		if _, err := ex.ExecContext(ctx, `INSERT INTO channel_short_names(channel_id,position,name) VALUES(?,?,?)`, c.ID, i, name); err != nil {
			return err
		}
	}
	return nil
}

func locationArgs(l *domain.Location) (lat, lon, radius any) {
	if l == nil {
		return nil, nil, nil
	}
	if l.RadiusKM != nil {
		radius = *l.RadiusKM
	}
	return l.Latitude, l.Longitude, radius
}

type toneArg struct {
	kind  string
	ctcss any
	dcs   any
	inv   bool
}

func toneArgs(t domain.Tone) toneArg {
	a := toneArg{kind: string(t.Kind), inv: t.Inverted}
	if t.CTCSSMilliHz != nil {
		a.ctcss = *t.CTCSSMilliHz
	}
	if t.DCSCode != nil {
		a.dcs = *t.DCSCode
	}
	return a
}

func (l *library) DeleteChannel(ctx context.Context, id string) error {
	r, err := l.q.ExecContext(ctx, `DELETE FROM channels WHERE id=? AND library_id=?`, id, l.id)
	if err != nil {
		return fmt.Errorf("delete channel: %w", err)
	}
	if n, _ := r.RowsAffected(); n == 0 {
		return fmt.Errorf("channel %q: %w", id, store.ErrNotFound)
	}
	return nil
}

func (l *library) GetChannel(ctx context.Context, id string) (domain.Channel, error) {
	chs, err := l.listChannels(ctx, ` AND c.id=?`, id)
	if err != nil {
		return domain.Channel{}, err
	}
	if len(chs) == 0 {
		return domain.Channel{}, fmt.Errorf("channel %q: %w", id, store.ErrNotFound)
	}
	return chs[0], nil
}

func (l *library) ListChannels(ctx context.Context) ([]domain.Channel, error) {
	return l.listChannels(ctx, "")
}

// listSeparator joins a channel's tags and short names in the list query.
// The ASCII unit separator can't appear in one typed or imported as text.
const listSeparator = "\x1f"

// listChannels returns the library's channels, narrowed by and, a SQL
// fragment starting with AND.
func (l *library) listChannels(ctx context.Context, and string, args ...any) ([]domain.Channel, error) {
	q := `SELECT c.id,c.latitude,c.longitude,c.radius_km,c.name,c.receive_hz,c.transmit_hz,c.mode,c.service,c.rx_only,c.receive_tone_kind,c.receive_ctcss_millihz,c.receive_dcs_code,c.receive_dcs_inverted,c.transmit_tone_kind,c.transmit_ctcss_millihz,c.transmit_dcs_code,c.transmit_dcs_inverted,c.tone_squelch,c.power,c.bandwidth,c.skip,c.notes,c.created_at,c.updated_at,(SELECT group_concat(t.tag, char(31) ORDER BY t.tag) FROM channel_tags t WHERE t.channel_id=c.id),(SELECT group_concat(n.name, char(31) ORDER BY n.position) FROM channel_short_names n WHERE n.channel_id=c.id) FROM channels c WHERE c.library_id=?` + and + ` ORDER BY c.name,c.receive_hz`
	rows, err := l.q.QueryContext(ctx, q, append([]any{l.id}, args...)...)
	if err != nil {
		return nil, fmt.Errorf("list channels: %w", err)
	}
	defer rows.Close()
	var out []domain.Channel
	for rows.Next() {
		c, err := scanChannel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	// SQL's order breaks ties stably; numbers in names need Go's.
	slices.SortStableFunc(out, domain.CompareChannels)
	return out, rows.Err()
}

func scanChannel(r scanner) (domain.Channel, error) {
	var c domain.Channel
	var rtCT, ttCT sql.NullInt64
	var rtD, ttD sql.NullInt64
	var rtInv, ttInv, ts, skip bool
	var created, updated string
	var tags, shortNames sql.NullString
	var lat, lon, radius sql.NullFloat64
	err := r.Scan(&c.ID, &lat, &lon, &radius, &c.Name, &c.ReceiveHz, &c.TransmitHz, &c.Mode, &c.Service, &c.RXOnly, &c.ReceiveTone.Kind, &rtCT, &rtD, &rtInv, &c.TransmitTone.Kind, &ttCT, &ttD, &ttInv, &ts, &c.Power, &c.Bandwidth, &skip, &c.Notes, &created, &updated, &tags, &shortNames)
	if err != nil {
		return c, err
	}
	if lat.Valid && lon.Valid {
		c.Location = &domain.Location{Latitude: lat.Float64, Longitude: lon.Float64}
		if radius.Valid {
			c.Location.RadiusKM = &radius.Float64
		}
	}
	if rtCT.Valid {
		c.ReceiveTone.CTCSSMilliHz = &rtCT.Int64
	}
	if rtD.Valid {
		x := int(rtD.Int64)
		c.ReceiveTone.DCSCode = &x
	}
	c.ReceiveTone.Inverted = rtInv
	if ttCT.Valid {
		c.TransmitTone.CTCSSMilliHz = &ttCT.Int64
	}
	if ttD.Valid {
		x := int(ttD.Int64)
		c.TransmitTone.DCSCode = &x
	}
	c.TransmitTone.Inverted = ttInv
	c.ToneSquelch = ts
	c.Skip = skip
	c.CreatedAt, c.UpdatedAt = parseTime(created), parseTime(updated)
	if shortNames.Valid && shortNames.String != "" {
		c.ShortNames = strings.Split(shortNames.String, listSeparator)
	}
	if tags.Valid && tags.String != "" {
		c.Tags = strings.Split(tags.String, listSeparator)
	}
	return c, nil
}
