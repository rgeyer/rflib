package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/ryan/rflib/internal/domain"
	_ "modernc.org/sqlite"
)

// ErrNotFound is returned (wrapped) when a group or channel lookup matches nothing.
var ErrNotFound = errors.New("not found")

type Store struct{ db *sql.DB }

type ChannelFilter struct {
	GroupID   string
	Tag       string
	Latitude  *float64
	Longitude *float64
	RadiusKM  *float64
}

// execer is satisfied by both *sql.DB and *sql.Tx.
type execer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

// Open opens a SQLite library. Pragmas are set in the DSN so every pooled
// connection enforces foreign keys, not just the first one.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("open database: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Ready(ctx context.Context) error {
	var version sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `SELECT MAX(version) FROM schema_migrations`).Scan(&version); err != nil {
		return fmt.Errorf("library is not initialized; run rflib init: %w", err)
	}
	if !version.Valid || version.Int64 < latestVersion() {
		return fmt.Errorf("library schema is out of date; run rflib init to upgrade it")
	}
	return nil
}

type migration struct {
	version int64
	sql     string
}

// baselineVersion is the schema version that baselineSchema creates directly
// on a fresh database.
const baselineVersion = 2

const baselineSchema = `
CREATE TABLE groups (id TEXT PRIMARY KEY, name TEXT NOT NULL UNIQUE, description TEXT NOT NULL DEFAULT '', latitude REAL, longitude REAL, radius_km REAL, notes TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL);
CREATE TABLE channels (id TEXT PRIMARY KEY, group_id TEXT REFERENCES groups(id) ON DELETE SET NULL, name TEXT NOT NULL, receive_hz INTEGER NOT NULL, transmit_hz INTEGER, mode TEXT NOT NULL, receive_tone_kind TEXT NOT NULL, receive_ctcss_millihz INTEGER, receive_dcs_code INTEGER, receive_dcs_inverted INTEGER NOT NULL DEFAULT 0, transmit_tone_kind TEXT NOT NULL, transmit_ctcss_millihz INTEGER, transmit_dcs_code INTEGER, transmit_dcs_inverted INTEGER NOT NULL DEFAULT 0, tone_squelch INTEGER NOT NULL DEFAULT 0, power TEXT NOT NULL DEFAULT '', bandwidth TEXT NOT NULL DEFAULT '', skip INTEGER NOT NULL DEFAULT 0, notes TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL);
CREATE TABLE channel_tags (channel_id TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE, tag TEXT NOT NULL, PRIMARY KEY(channel_id, tag));
CREATE INDEX idx_channels_group ON channels(group_id);
CREATE INDEX idx_channel_tags_tag ON channel_tags(tag);`

// migrations upgrade existing databases. Append new steps in version order;
// each runs in its own transaction together with its schema_migrations row.
var migrations = []migration{
	{2, `
ALTER TABLE channels ADD COLUMN receive_ctcss_millihz INTEGER;
ALTER TABLE channels ADD COLUMN transmit_ctcss_millihz INTEGER;
UPDATE channels SET receive_ctcss_millihz = CAST(ROUND(receive_ctcss_hz * 1000) AS INTEGER), transmit_ctcss_millihz = CAST(ROUND(transmit_ctcss_hz * 1000) AS INTEGER);`},
}

func latestVersion() int64 {
	v := int64(baselineVersion)
	for _, m := range migrations {
		v = max(v, m.version)
	}
	return v
}

func (s *Store) Migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY)`); err != nil {
		return fmt.Errorf("create migration table: %w", err)
	}
	var version sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `SELECT MAX(version) FROM schema_migrations`).Scan(&version); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if !version.Valid {
		if err := s.inTx(ctx, func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, baselineSchema); err != nil {
				return err
			}
			_, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version) VALUES (?)`, baselineVersion)
			return err
		}); err != nil {
			return fmt.Errorf("create schema: %w", err)
		}
		version = sql.NullInt64{Int64: baselineVersion, Valid: true}
	}
	for _, m := range migrations {
		if m.version <= version.Int64 {
			continue
		}
		if err := s.inTx(ctx, func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, m.sql); err != nil {
				return err
			}
			_, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version) VALUES (?)`, m.version)
			return err
		}); err != nil {
			return fmt.Errorf("migrate schema to version %d: %w", m.version, err)
		}
	}
	return nil
}

func (s *Store) inTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (s *Store) CreateGroup(ctx context.Context, g domain.Group) (domain.Group, error) {
	if err := domain.ValidateGroup(g); err != nil {
		return g, err
	}
	id, err := domain.NewID()
	if err != nil {
		return g, err
	}
	now := time.Now().UTC()
	g.ID = id
	g.CreatedAt = now
	g.UpdatedAt = now
	_, err = s.db.ExecContext(ctx, `INSERT INTO groups (id,name,description,latitude,longitude,radius_km,notes,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?)`, g.ID, g.Name, g.Description, g.Latitude, g.Longitude, g.RadiusKM, g.Notes, g.CreatedAt.Format(time.RFC3339Nano), g.UpdatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return g, fmt.Errorf("create group: %w", err)
	}
	return g, nil
}

// GetGroup looks a group up by ID or by its unique name.
func (s *Store) GetGroup(ctx context.Context, idOrName string) (domain.Group, error) {
	g, err := scanGroup(s.db.QueryRowContext(ctx, `SELECT id,name,description,latitude,longitude,radius_km,notes,created_at,updated_at FROM groups WHERE id=? OR name=?`, idOrName, idOrName))
	if errors.Is(err, sql.ErrNoRows) {
		return g, fmt.Errorf("group %q: %w", idOrName, ErrNotFound)
	}
	return g, err
}
func (s *Store) ListGroups(ctx context.Context) ([]domain.Group, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,description,latitude,longitude,radius_km,notes,created_at,updated_at FROM groups ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Group
	for rows.Next() {
		g, err := scanGroup(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}
func (s *Store) UpdateGroup(ctx context.Context, g domain.Group) (domain.Group, error) {
	if err := domain.ValidateGroup(g); err != nil {
		return g, err
	}
	g.UpdatedAt = time.Now().UTC()
	r, err := s.db.ExecContext(ctx, `UPDATE groups SET name=?,description=?,latitude=?,longitude=?,radius_km=?,notes=?,updated_at=? WHERE id=?`, g.Name, g.Description, g.Latitude, g.Longitude, g.RadiusKM, g.Notes, g.UpdatedAt.Format(time.RFC3339Nano), g.ID)
	if err != nil {
		return g, fmt.Errorf("update group: %w", err)
	}
	if n, _ := r.RowsAffected(); n == 0 {
		return g, fmt.Errorf("group %q: %w", g.ID, ErrNotFound)
	}
	return g, nil
}

// DeleteGroup deletes a group by ID or name. Its channels are kept and
// become ungrouped.
func (s *Store) DeleteGroup(ctx context.Context, idOrName string) error {
	r, err := s.db.ExecContext(ctx, `DELETE FROM groups WHERE id=? OR name=?`, idOrName, idOrName)
	if err != nil {
		return fmt.Errorf("delete group: %w", err)
	}
	if n, _ := r.RowsAffected(); n == 0 {
		return fmt.Errorf("group %q: %w", idOrName, ErrNotFound)
	}
	return nil
}

func (s *Store) CreateChannel(ctx context.Context, c domain.Channel) (domain.Channel, error) {
	c.Mode = strings.ToUpper(strings.TrimSpace(c.Mode))
	if err := domain.ValidateChannel(c); err != nil {
		return c, err
	}
	id, err := domain.NewID()
	if err != nil {
		return c, err
	}
	c.ID = id
	c.Tags = domain.NormalizeTags(c.Tags)
	now := time.Now().UTC()
	c.CreatedAt = now
	c.UpdatedAt = now
	err = s.inTx(ctx, func(tx *sql.Tx) error {
		if err := writeChannel(ctx, tx, `INSERT INTO channels (id,group_id,name,receive_hz,transmit_hz,mode,receive_tone_kind,receive_ctcss_millihz,receive_dcs_code,receive_dcs_inverted,transmit_tone_kind,transmit_ctcss_millihz,transmit_dcs_code,transmit_dcs_inverted,tone_squelch,power,bandwidth,skip,notes,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, c); err != nil {
			return err
		}
		return replaceTags(ctx, tx, c.ID, c.Tags)
	})
	if err != nil {
		return c, fmt.Errorf("create channel: %w", err)
	}
	return c, nil
}
func (s *Store) UpdateChannel(ctx context.Context, c domain.Channel) (domain.Channel, error) {
	c.Mode = strings.ToUpper(strings.TrimSpace(c.Mode))
	if err := domain.ValidateChannel(c); err != nil {
		return c, err
	}
	c.Tags = domain.NormalizeTags(c.Tags)
	c.UpdatedAt = time.Now().UTC()
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		if err := writeChannel(ctx, tx, `UPDATE channels SET group_id=?,name=?,receive_hz=?,transmit_hz=?,mode=?,receive_tone_kind=?,receive_ctcss_millihz=?,receive_dcs_code=?,receive_dcs_inverted=?,transmit_tone_kind=?,transmit_ctcss_millihz=?,transmit_dcs_code=?,transmit_dcs_inverted=?,tone_squelch=?,power=?,bandwidth=?,skip=?,notes=?,updated_at=? WHERE id=?`, c); err != nil {
			return err
		}
		return replaceTags(ctx, tx, c.ID, c.Tags)
	})
	if err != nil {
		return c, fmt.Errorf("update channel: %w", err)
	}
	return c, nil
}
func writeChannel(ctx context.Context, ex execer, q string, c domain.Channel) error {
	rt, tt := toneArgs(c.ReceiveTone), toneArgs(c.TransmitTone)
	args := []any{c.GroupID, c.Name, c.ReceiveHz, c.TransmitHz, c.Mode, rt.kind, rt.ctcss, rt.dcs, rt.inv, tt.kind, tt.ctcss, tt.dcs, tt.inv, c.ToneSquelch, c.Power, c.Bandwidth, c.Skip, c.Notes}
	insert := strings.HasPrefix(q, "INSERT")
	if insert {
		args = append([]any{c.ID}, args...)
		args = append(args, c.CreatedAt.Format(time.RFC3339Nano), c.UpdatedAt.Format(time.RFC3339Nano))
	} else {
		args = append(args, c.UpdatedAt.Format(time.RFC3339Nano), c.ID)
	}
	r, err := ex.ExecContext(ctx, q, args...)
	if err != nil {
		return err
	}
	if n, _ := r.RowsAffected(); !insert && n == 0 {
		return fmt.Errorf("channel %q: %w", c.ID, ErrNotFound)
	}
	return nil
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
func replaceTags(ctx context.Context, ex execer, id string, tags []string) error {
	if _, err := ex.ExecContext(ctx, `DELETE FROM channel_tags WHERE channel_id=?`, id); err != nil {
		return err
	}
	for _, tag := range tags {
		if _, err := ex.ExecContext(ctx, `INSERT INTO channel_tags(channel_id,tag) VALUES(?,?)`, id, tag); err != nil {
			return err
		}
	}
	return nil
}
func (s *Store) DeleteChannel(ctx context.Context, id string) error {
	r, err := s.db.ExecContext(ctx, `DELETE FROM channels WHERE id=?`, id)
	if err != nil {
		return fmt.Errorf("delete channel: %w", err)
	}
	if n, _ := r.RowsAffected(); n == 0 {
		return fmt.Errorf("channel %q: %w", id, ErrNotFound)
	}
	return nil
}

func (s *Store) GetChannel(ctx context.Context, id string) (domain.Channel, error) {
	chs, err := s.listChannels(ctx, ` WHERE c.id=?`, []any{id})
	if err != nil {
		return domain.Channel{}, err
	}
	if len(chs) == 0 {
		return domain.Channel{}, fmt.Errorf("channel %q: %w", id, ErrNotFound)
	}
	return chs[0], nil
}
func (s *Store) ListChannels(ctx context.Context, f ChannelFilter) ([]domain.Channel, error) {
	geo := f.Latitude != nil || f.Longitude != nil || f.RadiusKM != nil
	if geo && (f.Latitude == nil || f.Longitude == nil || f.RadiusKM == nil) {
		return nil, fmt.Errorf("latitude, longitude, and radius must be supplied together")
	}
	q := ` WHERE 1=1`
	args := []any{}
	if f.GroupID != "" {
		q += ` AND c.group_id=?`
		args = append(args, f.GroupID)
	}
	if f.Tag != "" {
		q += ` AND EXISTS (SELECT 1 FROM channel_tags ft WHERE ft.channel_id=c.id AND ft.tag=?)`
		args = append(args, strings.ToLower(strings.TrimSpace(f.Tag)))
	}
	chs, err := s.listChannels(ctx, q, args)
	if err != nil || !geo {
		return chs, err
	}
	out := chs[:0]
	for _, c := range chs {
		if c.Group != nil && c.Group.Latitude != nil && distanceKM(*f.Latitude, *f.Longitude, *c.Group.Latitude, *c.Group.Longitude) <= *f.RadiusKM {
			out = append(out, c)
		}
	}
	return out, nil
}

// tagSeparator joins a channel's tags in the list query. The ASCII unit
// separator cannot appear in a normalized tag typed or imported as text.
const tagSeparator = "\x1f"

func (s *Store) listChannels(ctx context.Context, where string, args []any) ([]domain.Channel, error) {
	q := `SELECT c.id,c.group_id,c.name,c.receive_hz,c.transmit_hz,c.mode,c.receive_tone_kind,c.receive_ctcss_millihz,c.receive_dcs_code,c.receive_dcs_inverted,c.transmit_tone_kind,c.transmit_ctcss_millihz,c.transmit_dcs_code,c.transmit_dcs_inverted,c.tone_squelch,c.power,c.bandwidth,c.skip,c.notes,c.created_at,c.updated_at,g.id,g.name,g.description,g.latitude,g.longitude,g.radius_km,g.notes,g.created_at,g.updated_at,(SELECT group_concat(t.tag, char(31) ORDER BY t.tag) FROM channel_tags t WHERE t.channel_id=c.id) FROM channels c LEFT JOIN groups g ON c.group_id=g.id` + where + ` ORDER BY c.name,c.receive_hz`
	rows, err := s.db.QueryContext(ctx, q, args...)
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
	return out, rows.Err()
}

type scanner interface{ Scan(...any) error }

func scanGroup(r scanner) (domain.Group, error) {
	var g domain.Group
	var created, updated string
	err := r.Scan(&g.ID, &g.Name, &g.Description, &g.Latitude, &g.Longitude, &g.RadiusKM, &g.Notes, &created, &updated)
	if err != nil {
		return g, err
	}
	g.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	g.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return g, nil
}
func scanChannel(r scanner) (domain.Channel, error) {
	var c domain.Channel
	var rtCT, ttCT sql.NullInt64
	var rtD, ttD sql.NullInt64
	var rtInv, ttInv, ts, skip bool
	var created, updated string
	var gid sql.NullString
	var g domain.Group
	var groupID, gn, gd, gnote, gc, gu, tags sql.NullString
	var glat, glon, grad sql.NullFloat64
	err := r.Scan(&c.ID, &gid, &c.Name, &c.ReceiveHz, &c.TransmitHz, &c.Mode, &c.ReceiveTone.Kind, &rtCT, &rtD, &rtInv, &c.TransmitTone.Kind, &ttCT, &ttD, &ttInv, &ts, &c.Power, &c.Bandwidth, &skip, &c.Notes, &created, &updated, &groupID, &gn, &gd, &glat, &glon, &grad, &gnote, &gc, &gu, &tags)
	if err != nil {
		return c, err
	}
	if gid.Valid {
		c.GroupID = &gid.String
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
	c.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	c.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	if tags.Valid && tags.String != "" {
		c.Tags = strings.Split(tags.String, tagSeparator)
	}
	if groupID.Valid {
		g.ID = groupID.String
		g.Name = gn.String
		g.Description = gd.String
		g.Notes = gnote.String
		if glat.Valid {
			g.Latitude = &glat.Float64
			g.Longitude = &glon.Float64
		}
		if grad.Valid {
			g.RadiusKM = &grad.Float64
		}
		g.CreatedAt, _ = time.Parse(time.RFC3339Nano, gc.String)
		g.UpdatedAt, _ = time.Parse(time.RFC3339Nano, gu.String)
		c.Group = &g
	}
	return c, nil
}
func distanceKM(lat1, lon1, lat2, lon2 float64) float64 {
	const r = 6371
	dLat := (lat2 - lat1) * math.Pi / 180
	dLon := (lon2 - lon1) * math.Pi / 180
	a := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(lat1*math.Pi/180)*math.Cos(lat2*math.Pi/180)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return r * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}
