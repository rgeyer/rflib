// Package sqlite is the SQLite implementation of package store, using the
// pure-Go modernc.org/sqlite driver.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ryan/rflib/internal/domain"
	"github.com/ryan/rflib/internal/query"
	"github.com/ryan/rflib/internal/store"
	_ "modernc.org/sqlite"
)

// DB is a SQLite database of libraries.
type DB struct {
	db *sql.DB
}

var _ store.Store = (*DB)(nil)

// execer and querier are satisfied by both *sql.DB and *sql.Tx.
type execer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

type querier interface {
	execer
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// Open opens a SQLite database. Pragmas are set in the DSN so every pooled
// connection enforces foreign keys, not just the first one.
func Open(path string) (*DB, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("open database: %w", err)
	}
	return &DB{db: db}, nil
}

func (d *DB) Close() error { return d.db.Close() }

func (d *DB) Ready(ctx context.Context) error {
	var version sql.NullInt64
	if err := d.db.QueryRowContext(ctx, `SELECT MAX(version) FROM schema_migrations`).Scan(&version); err != nil {
		return fmt.Errorf("library is not initialized; run rflib init: %w", err)
	}
	if !version.Valid || version.Int64 < latestVersion() {
		return domain.Invalidf("library schema is out of date; run rflib init to upgrade it")
	}
	return nil
}

type migration struct {
	version int64
	sql     string
	// rebuild marks a migration that rebuilds tables other tables refer
	// to. It runs with foreign keys off, as SQLite's ALTER TABLE
	// documentation prescribes, and checks them before committing.
	rebuild bool
	// after, when set, runs in the same transaction after sql, for steps
	// SQL can't express.
	after func(context.Context, *sql.Tx) error
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

// newIDSQL generates an ID in the same shape as domain.NewID.
const newIDSQL = `lower(hex(randomblob(4)) || '-' || hex(randomblob(2)) || '-' || hex(randomblob(2)) || '-' || hex(randomblob(2)) || '-' || hex(randomblob(6)))`

// migrations upgrade existing databases. Append new steps in version order;
// each runs in its own transaction together with its schema_migrations row.
var migrations = []migration{
	{version: 2, sql: `
ALTER TABLE channels ADD COLUMN receive_ctcss_millihz INTEGER;
ALTER TABLE channels ADD COLUMN transmit_ctcss_millihz INTEGER;
UPDATE channels SET receive_ctcss_millihz = CAST(ROUND(receive_ctcss_hz * 1000) AS INTEGER), transmit_ctcss_millihz = CAST(ROUND(transmit_ctcss_hz * 1000) AS INTEGER);`},
	{version: 3, sql: `
ALTER TABLE channels ADD COLUMN service TEXT NOT NULL DEFAULT '';
ALTER TABLE channels ADD COLUMN rx_only INTEGER NOT NULL DEFAULT 0;
CREATE INDEX idx_channels_service ON channels(service);`},
	{version: 4, sql: `
CREATE TABLE channel_short_names (channel_id TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE, position INTEGER NOT NULL, name TEXT NOT NULL, PRIMARY KEY(channel_id, position));`},
	// Power becomes an intent. Level names map across; anything else,
	// such as watts from an import, describes the source's radio and is
	// dropped (see domain.PowerFromText, which imports use).
	{version: 5, sql: `
UPDATE channels SET power = CASE lower(trim(power))
	WHEN 'low' THEN 'low' WHEN 'lo' THEN 'low' WHEN 'l' THEN 'low' WHEN 'low1' THEN 'low' WHEN 'low2' THEN 'low'
	WHEN 'medium' THEN 'medium' WHEN 'med' THEN 'medium' WHEN 'mid' THEN 'medium' WHEN 'm' THEN 'medium' WHEN 'mid1' THEN 'medium' WHEN 'mid2' THEN 'medium'
	WHEN 'high' THEN 'high' WHEN 'hi' THEN 'high' WHEN 'h' THEN 'high'
	WHEN 'max' THEN 'max' WHEN 'maximum' THEN 'max' WHEN 'full' THEN 'max'
	ELSE '' END;
ALTER TABLE groups ADD COLUMN power TEXT NOT NULL DEFAULT '';
CREATE TABLE radios (id TEXT PRIMARY KEY, name TEXT NOT NULL UNIQUE, model TEXT NOT NULL, default_power TEXT NOT NULL DEFAULT '', dedupe TEXT NOT NULL DEFAULT '', sync_tag TEXT NOT NULL DEFAULT '', sync_group_id TEXT REFERENCES groups(id) ON DELETE SET NULL, sync_service TEXT NOT NULL DEFAULT '', notes TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL);
CREATE TABLE radio_channels (radio_id TEXT NOT NULL REFERENCES radios(id) ON DELETE CASCADE, channel_id TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE, position INTEGER NOT NULL, section_start INTEGER, name TEXT NOT NULL DEFAULT '', power TEXT NOT NULL DEFAULT '', skip INTEGER, PRIMARY KEY(radio_id, channel_id));
CREATE INDEX idx_radio_channels_order ON radio_channels(radio_id, position);
CREATE INDEX idx_radio_channels_channel ON radio_channels(channel_id);`},
	// Saved searches, and radio sources in place of the one-filter sync.
	// Each radio's old sync filter becomes its first source.
	{version: 6, sql: `
CREATE TABLE searches (id TEXT PRIMARY KEY, name TEXT NOT NULL UNIQUE, query TEXT NOT NULL, notes TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL);
CREATE TABLE radio_sources (id TEXT PRIMARY KEY, radio_id TEXT NOT NULL REFERENCES radios(id) ON DELETE CASCADE, position INTEGER NOT NULL, query TEXT NOT NULL, sort TEXT NOT NULL DEFAULT 'name', section_start INTEGER);
CREATE INDEX idx_radio_sources_order ON radio_sources(radio_id, position);
ALTER TABLE radio_channels ADD COLUMN source_id TEXT REFERENCES radio_sources(id) ON DELETE SET NULL;
INSERT INTO radio_sources (id, radio_id, position, query, sort)
SELECT ` + newIDSQL + `, id, 0,
	trim(
		CASE WHEN sync_tag <> '' THEN 'tag:"' || replace(replace(sync_tag, '\', '\\'), '"', '\"') || '" ' ELSE '' END ||
		CASE WHEN sync_group_id IS NOT NULL THEN 'group:' || sync_group_id || ' ' ELSE '' END ||
		CASE WHEN sync_service <> '' THEN 'service:' || sync_service ELSE '' END),
	'name'
FROM radios WHERE sync_tag <> '' OR sync_group_id IS NOT NULL OR sync_service <> '';`},
	// Libraries. Everything so far moves into one named "default". Groups,
	// channels, radios, and searches are rebuilt with a library_id, so
	// names are unique per library; the rebuild also drops the unused
	// sync_* and pre-v2 CTCSS columns.
	{version: 7, rebuild: true, sql: `
CREATE TABLE libraries (id TEXT PRIMARY KEY, name TEXT NOT NULL UNIQUE, description TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL);
INSERT INTO libraries (id, name, created_at, updated_at) VALUES (` + newIDSQL + `, 'default', strftime('%Y-%m-%dT%H:%M:%fZ'), strftime('%Y-%m-%dT%H:%M:%fZ'));

CREATE TABLE new_groups (id TEXT PRIMARY KEY, library_id TEXT NOT NULL REFERENCES libraries(id) ON DELETE CASCADE, name TEXT NOT NULL, description TEXT NOT NULL DEFAULT '', latitude REAL, longitude REAL, radius_km REAL, power TEXT NOT NULL DEFAULT '', notes TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL, UNIQUE(library_id, name));
INSERT INTO new_groups SELECT id, (SELECT id FROM libraries), name, description, latitude, longitude, radius_km, power, notes, created_at, updated_at FROM groups;
DROP TABLE groups;
ALTER TABLE new_groups RENAME TO groups;

CREATE TABLE new_channels (id TEXT PRIMARY KEY, library_id TEXT NOT NULL REFERENCES libraries(id) ON DELETE CASCADE, group_id TEXT REFERENCES groups(id) ON DELETE SET NULL, name TEXT NOT NULL, receive_hz INTEGER NOT NULL, transmit_hz INTEGER, mode TEXT NOT NULL, service TEXT NOT NULL DEFAULT '', rx_only INTEGER NOT NULL DEFAULT 0, receive_tone_kind TEXT NOT NULL, receive_ctcss_millihz INTEGER, receive_dcs_code INTEGER, receive_dcs_inverted INTEGER NOT NULL DEFAULT 0, transmit_tone_kind TEXT NOT NULL, transmit_ctcss_millihz INTEGER, transmit_dcs_code INTEGER, transmit_dcs_inverted INTEGER NOT NULL DEFAULT 0, tone_squelch INTEGER NOT NULL DEFAULT 0, power TEXT NOT NULL DEFAULT '', bandwidth TEXT NOT NULL DEFAULT '', skip INTEGER NOT NULL DEFAULT 0, notes TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL);
INSERT INTO new_channels SELECT id, (SELECT id FROM libraries), group_id, name, receive_hz, transmit_hz, mode, service, rx_only, receive_tone_kind, receive_ctcss_millihz, receive_dcs_code, receive_dcs_inverted, transmit_tone_kind, transmit_ctcss_millihz, transmit_dcs_code, transmit_dcs_inverted, tone_squelch, power, bandwidth, skip, notes, created_at, updated_at FROM channels;
DROP TABLE channels;
ALTER TABLE new_channels RENAME TO channels;
CREATE INDEX idx_channels_library ON channels(library_id, name);
CREATE INDEX idx_channels_group ON channels(group_id);
CREATE INDEX idx_channels_service ON channels(service);

CREATE TABLE new_radios (id TEXT PRIMARY KEY, library_id TEXT NOT NULL REFERENCES libraries(id) ON DELETE CASCADE, name TEXT NOT NULL, model TEXT NOT NULL, default_power TEXT NOT NULL DEFAULT '', dedupe TEXT NOT NULL DEFAULT '', notes TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL, UNIQUE(library_id, name));
INSERT INTO new_radios SELECT id, (SELECT id FROM libraries), name, model, default_power, dedupe, notes, created_at, updated_at FROM radios;
DROP TABLE radios;
ALTER TABLE new_radios RENAME TO radios;

CREATE TABLE new_searches (id TEXT PRIMARY KEY, library_id TEXT NOT NULL REFERENCES libraries(id) ON DELETE CASCADE, name TEXT NOT NULL, query TEXT NOT NULL, notes TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL, UNIQUE(library_id, name));
INSERT INTO new_searches SELECT id, (SELECT id FROM libraries), name, query, notes, created_at, updated_at FROM searches;
DROP TABLE searches;
ALTER TABLE new_searches RENAME TO searches;`},
	// Sources that re-sort their channels on every sync.
	{version: 8, sql: `ALTER TABLE radio_sources ADD COLUMN keep_sorted INTEGER NOT NULL DEFAULT 0;`},
	// Groups give way to channel locations and tags: each grouped channel
	// takes its group's location and power where it has none, and a tag
	// named after the group (see ungroup), and group: in saved queries
	// becomes tag:. The group sort becomes name.
	{version: 9, rebuild: true, after: ungroup, sql: `
CREATE TABLE new_channels (id TEXT PRIMARY KEY, library_id TEXT NOT NULL REFERENCES libraries(id) ON DELETE CASCADE, latitude REAL, longitude REAL, radius_km REAL, name TEXT NOT NULL, receive_hz INTEGER NOT NULL, transmit_hz INTEGER, mode TEXT NOT NULL, service TEXT NOT NULL DEFAULT '', rx_only INTEGER NOT NULL DEFAULT 0, receive_tone_kind TEXT NOT NULL, receive_ctcss_millihz INTEGER, receive_dcs_code INTEGER, receive_dcs_inverted INTEGER NOT NULL DEFAULT 0, transmit_tone_kind TEXT NOT NULL, transmit_ctcss_millihz INTEGER, transmit_dcs_code INTEGER, transmit_dcs_inverted INTEGER NOT NULL DEFAULT 0, tone_squelch INTEGER NOT NULL DEFAULT 0, power TEXT NOT NULL DEFAULT '', bandwidth TEXT NOT NULL DEFAULT '', skip INTEGER NOT NULL DEFAULT 0, notes TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL);
INSERT INTO new_channels SELECT c.id, c.library_id, g.latitude, g.longitude, CASE WHEN g.latitude IS NULL THEN NULL ELSE g.radius_km END, c.name, c.receive_hz, c.transmit_hz, c.mode, c.service, c.rx_only, c.receive_tone_kind, c.receive_ctcss_millihz, c.receive_dcs_code, c.receive_dcs_inverted, c.transmit_tone_kind, c.transmit_ctcss_millihz, c.transmit_dcs_code, c.transmit_dcs_inverted, c.tone_squelch, CASE WHEN c.power = '' THEN coalesce(g.power, '') ELSE c.power END, c.bandwidth, c.skip, c.notes, c.created_at, c.updated_at
	FROM channels c LEFT JOIN groups g ON g.id = c.group_id;
UPDATE radio_sources SET sort = 'name' WHERE sort = 'group';`},
	// Shared channels: one shared library (a library of the user's named
	// "shared" is renamed out of the way), links to its channels, their
	// status, and their revisions.
	{version: 10, sql: `
ALTER TABLE libraries ADD COLUMN kind TEXT NOT NULL DEFAULT '';
UPDATE libraries SET name = name || '-1' WHERE name = 'shared';
INSERT INTO libraries (id, name, description, kind, created_at, updated_at) VALUES (` + newIDSQL + `, 'shared', 'Channels shared with everyone', 'shared', strftime('%Y-%m-%dT%H:%M:%fZ'), strftime('%Y-%m-%dT%H:%M:%fZ'));
ALTER TABLE channels ADD COLUMN link_id TEXT REFERENCES channels(id);
ALTER TABLE channels ADD COLUMN seen_revision INTEGER NOT NULL DEFAULT 0;
ALTER TABLE channels ADD COLUMN status TEXT NOT NULL DEFAULT 'active';
ALTER TABLE channels ADD COLUMN status_note TEXT NOT NULL DEFAULT '';
ALTER TABLE channels ADD COLUMN back_on TEXT;
ALTER TABLE channels ADD COLUMN replaced_by TEXT;
ALTER TABLE channels ADD COLUMN revision INTEGER NOT NULL DEFAULT 0;
ALTER TABLE channels ADD COLUMN copied_from TEXT NOT NULL DEFAULT '';
CREATE INDEX idx_channels_link ON channels(link_id);
CREATE TABLE channel_revisions (channel_id TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE, revision INTEGER NOT NULL, changed_at TEXT NOT NULL, reason TEXT NOT NULL DEFAULT '', author TEXT NOT NULL DEFAULT '', record TEXT NOT NULL, PRIMARY KEY(channel_id, revision));`},
	// The default library is marked rather than found by name, so it can be
	// renamed: the one named "default", else the oldest private library,
	// else a new "default" if there is no private library left.
	{version: 11, sql: `
ALTER TABLE libraries ADD COLUMN is_default INTEGER NOT NULL DEFAULT 0;
INSERT INTO libraries (id, name, description, kind, created_at, updated_at) SELECT ` + newIDSQL + `, 'default', '', '', strftime('%Y-%m-%dT%H:%M:%fZ'), strftime('%Y-%m-%dT%H:%M:%fZ') WHERE NOT EXISTS (SELECT 1 FROM libraries WHERE kind = '');
UPDATE libraries SET is_default = 1 WHERE id = coalesce((SELECT id FROM libraries WHERE name = 'default' AND kind = ''), (SELECT id FROM libraries WHERE kind = '' ORDER BY created_at, name LIMIT 1));
CREATE UNIQUE INDEX idx_libraries_default ON libraries(is_default) WHERE is_default = 1;`},
	// Organizations, systems, and plans, kept in the shared library;
	// network identifiers on channels and systems; revisions of records
	// other than channels; and plans as radio sources. The network tags
	// give way to network identifiers and systems, so they come off shared
	// channels (personal tags stay).
	{version: 12, sql: `
CREATE TABLE organizations (id TEXT PRIMARY KEY, library_id TEXT NOT NULL REFERENCES libraries(id) ON DELETE CASCADE, name TEXT NOT NULL, kind TEXT NOT NULL, callsign TEXT NOT NULL DEFAULT '', website TEXT NOT NULL DEFAULT '', notes TEXT NOT NULL DEFAULT '', revision INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL, updated_at TEXT NOT NULL, UNIQUE(library_id, name));
CREATE TABLE channel_networks (channel_id TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE, network TEXT NOT NULL, node TEXT NOT NULL, PRIMARY KEY(channel_id, network, node));
CREATE INDEX idx_channel_networks_node ON channel_networks(network, node);
CREATE TABLE channel_orgs (channel_id TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE, org_id TEXT NOT NULL REFERENCES organizations(id), role TEXT NOT NULL, PRIMARY KEY(channel_id, org_id, role));
CREATE INDEX idx_channel_orgs_org ON channel_orgs(org_id);
CREATE TABLE systems (id TEXT PRIMARY KEY, library_id TEXT NOT NULL REFERENCES libraries(id) ON DELETE CASCADE, name TEXT NOT NULL, link_type TEXT NOT NULL DEFAULT '', notes TEXT NOT NULL DEFAULT '', revision INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL, updated_at TEXT NOT NULL, UNIQUE(library_id, name));
CREATE TABLE system_networks (system_id TEXT NOT NULL REFERENCES systems(id) ON DELETE CASCADE, network TEXT NOT NULL, node TEXT NOT NULL, PRIMARY KEY(system_id, network, node));
CREATE TABLE system_orgs (system_id TEXT NOT NULL REFERENCES systems(id) ON DELETE CASCADE, org_id TEXT NOT NULL REFERENCES organizations(id), role TEXT NOT NULL, PRIMARY KEY(system_id, org_id, role));
CREATE INDEX idx_system_orgs_org ON system_orgs(org_id);
CREATE TABLE system_members (system_id TEXT NOT NULL REFERENCES systems(id) ON DELETE CASCADE, channel_id TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE, linking TEXT NOT NULL, notes TEXT NOT NULL DEFAULT '', PRIMARY KEY(system_id, channel_id));
CREATE INDEX idx_system_members_channel ON system_members(channel_id);
CREATE TABLE plans (id TEXT PRIMARY KEY, library_id TEXT NOT NULL REFERENCES libraries(id) ON DELETE CASCADE, name TEXT NOT NULL, description TEXT NOT NULL DEFAULT '', starts_on TEXT, ends_on TEXT, status TEXT NOT NULL, revision INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL, updated_at TEXT NOT NULL, UNIQUE(library_id, name));
CREATE TABLE plan_entries (plan_id TEXT NOT NULL REFERENCES plans(id) ON DELETE CASCADE, position INTEGER NOT NULL, channel_id TEXT REFERENCES channels(id) ON DELETE CASCADE, query TEXT NOT NULL DEFAULT '', sort TEXT NOT NULL DEFAULT '', name TEXT NOT NULL DEFAULT '', assignment TEXT NOT NULL DEFAULT '', notes TEXT NOT NULL DEFAULT '', section_start INTEGER, PRIMARY KEY(plan_id, position));
CREATE INDEX idx_plan_entries_channel ON plan_entries(channel_id);
CREATE TABLE record_revisions (kind TEXT NOT NULL, record_id TEXT NOT NULL, revision INTEGER NOT NULL, changed_at TEXT NOT NULL, reason TEXT NOT NULL DEFAULT '', author TEXT NOT NULL DEFAULT '', record TEXT NOT NULL, PRIMARY KEY(kind, record_id, revision));
ALTER TABLE radio_sources ADD COLUMN plan_id TEXT REFERENCES plans(id);
DELETE FROM channel_tags WHERE tag IN ('linked', 'allstar', 'echolink', 'wires-x') AND channel_id IN (SELECT id FROM channels WHERE link_id IS NULL AND library_id IN (SELECT id FROM libraries WHERE kind = 'shared'));`},
	// A radio's own name length, for models that don't know the radio's
	// (such as chirp-generic); 0 uses the model's.
	{version: 13, sql: `ALTER TABLE radios ADD COLUMN name_length INTEGER NOT NULL DEFAULT 0;`},
	// simplex is no longer a shared tag: is:simplex already finds simplex
	// channels. It comes off shared channels; links' own tags stay.
	{version: 14, sql: `DELETE FROM channel_tags WHERE tag = 'simplex' AND channel_id IN (SELECT id FROM channels WHERE link_id IS NULL AND library_id IN (SELECT id FROM libraries WHERE kind = 'shared'));`},
	// Which built-in list each seeded shared channel came from, and the row
	// as last applied.
	{version: 15, sql: `CREATE TABLE seed_channels (channel_id TEXT PRIMARY KEY REFERENCES channels(id) ON DELETE CASCADE, list TEXT NOT NULL, name TEXT NOT NULL, applied TEXT NOT NULL);`},
	// Radios hold shared channels directly, and a library follows the
	// shared channels it uses (see foldLinks).
	{version: 16, after: foldLinks, sql: `CREATE TABLE shared_follows (library_id TEXT NOT NULL REFERENCES libraries(id) ON DELETE CASCADE, shared_id TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE, seen_revision INTEGER NOT NULL, PRIMARY KEY(library_id, shared_id));`},
}

// ungroup finishes migration 9 once new_channels holds the channels: it
// tags each grouped channel with its group's name, rewrites group: terms in
// saved searches and radio sources to tag:, and swaps the tables.
// foldLinks turns links into what radios and annotations need now. A
// radio's entry for a link moves to its shared channel (dropped where the
// radio has that already), taking the link's power and skip where the
// radio had none of its own. The link's seen revision becomes a follow. A
// link with no tags, notes, or short names of its own goes; the rest stay
// as annotations, without power or skip.
func foldLinks(ctx context.Context, tx *sql.Tx) error {
	type link struct {
		id, library, shared, power string
		seen                       int
		skip                       bool
	}
	rows, err := tx.QueryContext(ctx, `SELECT id, library_id, link_id, seen_revision, power, skip FROM channels WHERE link_id IS NOT NULL`)
	if err != nil {
		return err
	}
	var links []link
	for rows.Next() {
		var l link
		if err := rows.Scan(&l.id, &l.library, &l.shared, &l.seen, &l.power, &l.skip); err != nil {
			_ = rows.Close()
			return err
		}
		links = append(links, l)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	radios := map[string]bool{}
	for _, l := range links {
		if _, err := tx.ExecContext(ctx, `INSERT INTO shared_follows (library_id, shared_id, seen_revision) VALUES (?,?,?)
			ON CONFLICT(library_id, shared_id) DO UPDATE SET seen_revision=max(seen_revision, excluded.seen_revision)`, l.library, l.shared, l.seen); err != nil {
			return err
		}
		entries, err := tx.QueryContext(ctx, `SELECT radio_id FROM radio_channels WHERE channel_id=?`, l.id)
		if err != nil {
			return err
		}
		var onRadios []string
		for entries.Next() {
			var r string
			if err := entries.Scan(&r); err != nil {
				_ = entries.Close()
				return err
			}
			onRadios = append(onRadios, r)
		}
		if err := entries.Close(); err != nil {
			return err
		}
		for _, r := range onRadios {
			radios[r] = true
			var has int
			if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM radio_channels WHERE radio_id=? AND channel_id=?`, r, l.shared).Scan(&has); err != nil {
				return err
			}
			if has > 0 {
				if _, err := tx.ExecContext(ctx, `DELETE FROM radio_channels WHERE radio_id=? AND channel_id=?`, r, l.id); err != nil {
					return err
				}
				continue
			}
			if _, err := tx.ExecContext(ctx, `UPDATE radio_channels SET channel_id=?,
				power=CASE WHEN power='' THEN ? ELSE power END,
				skip=CASE WHEN skip IS NULL AND ? THEN 1 ELSE skip END
				WHERE radio_id=? AND channel_id=?`, l.shared, l.power, l.skip, r, l.id); err != nil {
				return err
			}
		}
		var own int
		if err := tx.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM channel_tags WHERE channel_id=?) + (SELECT count(*) FROM channel_short_names WHERE channel_id=?)
			+ (SELECT count(*) FROM channels WHERE id=? AND trim(notes)<>'')`, l.id, l.id, l.id).Scan(&own); err != nil {
			return err
		}
		q := `UPDATE channels SET power='', skip=0, seen_revision=0 WHERE id=?`
		if own == 0 {
			q = `DELETE FROM channels WHERE id=?`
		}
		if _, err := tx.ExecContext(ctx, q, l.id); err != nil {
			return err
		}
	}
	// Close the gaps dropped entries left in radios' memory order.
	for r := range radios {
		order, err := radioOrder(ctx, tx, r)
		if err != nil {
			return err
		}
		if err := renumber(ctx, tx, r, order); err != nil {
			return err
		}
	}
	return nil
}

func ungroup(ctx context.Context, tx *sql.Tx) error {
	type group struct{ id, library, name string }
	var groups []group
	rows, err := tx.QueryContext(ctx, `SELECT id, library_id, name FROM groups`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var g group
		if err := rows.Scan(&g.id, &g.library, &g.name); err != nil {
			_ = rows.Close()
			return err
		}
		groups = append(groups, g)
	}
	_ = rows.Close()
	tag := func(name string) string { return domain.NormalizeTags([]string{name})[0] }
	for _, g := range groups {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO channel_tags (channel_id, tag) SELECT id, ? FROM channels WHERE group_id = ?`, tag(g.name), g.id); err != nil {
			return err
		}
	}
	// A group: term names a group by ID or name, in its own library.
	rewrite := func(library, text string) string {
		n, err := query.Parse(text)
		if err != nil {
			return text
		}
		return query.MapTerms(n, func(t query.Term) query.Term {
			if t.Field != "group" {
				return t
			}
			for _, g := range groups {
				if g.library == library && (t.Value == g.id || strings.EqualFold(t.Value, g.name)) {
					return query.Term{Field: "tag", Value: tag(g.name)}
				}
			}
			return t
		}).String()
	}
	for _, table := range []struct{ name, library string }{
		{"searches", "library_id"},
		{"radio_sources", "(SELECT library_id FROM radios WHERE radios.id = radio_sources.radio_id)"},
	} {
		type row struct{ id, library, query string }
		var rs []row
		rows, err := tx.QueryContext(ctx, `SELECT id, `+table.library+`, query FROM `+table.name+` WHERE query LIKE '%group:%'`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var r row
			if err := rows.Scan(&r.id, &r.library, &r.query); err != nil {
				_ = rows.Close()
				return err
			}
			rs = append(rs, r)
		}
		_ = rows.Close()
		for _, r := range rs {
			if q := rewrite(r.library, r.query); q != r.query {
				if _, err := tx.ExecContext(ctx, `UPDATE `+table.name+` SET query = ? WHERE id = ?`, q, r.id); err != nil {
					return err
				}
			}
		}
	}
	_, err = tx.ExecContext(ctx, `
DROP TABLE channels;
ALTER TABLE new_channels RENAME TO channels;
CREATE INDEX idx_channels_library ON channels(library_id, name);
CREATE INDEX idx_channels_service ON channels(service);
DROP TABLE groups;`)
	return err
}

func latestVersion() int64 {
	v := int64(baselineVersion)
	for _, m := range migrations {
		v = max(v, m.version)
	}
	return v
}

func (d *DB) Migrate(ctx context.Context) error {
	if _, err := d.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY)`); err != nil {
		return fmt.Errorf("create migration table: %w", err)
	}
	var version sql.NullInt64
	if err := d.db.QueryRowContext(ctx, `SELECT MAX(version) FROM schema_migrations`).Scan(&version); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if !version.Valid {
		if err := d.inTx(ctx, func(tx *sql.Tx) error {
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
		if err := d.migrate(ctx, m); err != nil {
			return fmt.Errorf("migrate schema to version %d: %w", m.version, err)
		}
	}
	return nil
}

func (d *DB) migrate(ctx context.Context, m migration) error {
	if m.rebuild {
		// The pool has one connection, so the pragma applies to the
		// transaction below. It can't change inside a transaction.
		if _, err := d.db.ExecContext(ctx, `PRAGMA foreign_keys=OFF`); err != nil {
			return err
		}
		defer func() { _, _ = d.db.ExecContext(context.WithoutCancel(ctx), `PRAGMA foreign_keys=ON`) }()
	}
	return d.inTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, m.sql); err != nil {
			return err
		}
		if m.after != nil {
			if err := m.after(ctx, tx); err != nil {
				return err
			}
		}
		if m.rebuild {
			if err := checkForeignKeys(ctx, tx); err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version) VALUES (?)`, m.version)
		return err
	})
}

// checkForeignKeys fails when any reference is broken.
func checkForeignKeys(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return err
	}
	defer rows.Close()
	if rows.Next() {
		var table, parent string
		var rowid sql.NullInt64
		var fk int
		if err := rows.Scan(&table, &rowid, &parent, &fk); err != nil {
			return err
		}
		return domain.Invalidf("broken reference from %s to %s", table, parent)
	}
	return rows.Err()
}

func (d *DB) inTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// assignIdentity fills in an ID and timestamps that the caller left empty.
// Imports supply them so a restored backup keeps its original identity.
func assignIdentity(id *string, created, updated *time.Time) error {
	if *id == "" {
		v, err := domain.NewID()
		if err != nil {
			return err
		}
		*id = v
	}
	now := time.Now().UTC()
	if created.IsZero() {
		*created = now
	}
	if updated.IsZero() {
		*updated = now
	}
	return nil
}

// freeID clears a supplied ID that a record in table already has, in any
// library, so assignIdentity gives a new one. Restoring one library's
// backup into another therefore copies it instead of failing.
func freeID(ctx context.Context, q querier, table string, id *string) error {
	if *id == "" {
		return nil
	}
	var n int
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM `+table+` WHERE id=?`, *id).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		*id = ""
	}
	return nil
}

// conflict turns a uniqueness violation into store.ErrConflict.
func conflict(err error, format string, args ...any) error {
	if err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed") {
		return fmt.Errorf(format+": %w", append(args, store.ErrConflict)...)
	}
	return err
}

func timestamp(t time.Time) string { return t.Format(time.RFC3339Nano) }

func parseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}

// --- libraries ---

const libraryColumns = `id,name,description,kind,is_default,created_at,updated_at`

// sharedKind is the kind of the shared library.
const sharedKind = "shared"

func (d *DB) CreateLibrary(ctx context.Context, lib domain.Library) (domain.Library, error) {
	if err := domain.ValidateLibrary(lib); err != nil {
		return lib, err
	}
	if lib.Shared {
		return lib, fmt.Errorf("there is only one shared library: %w", store.ErrConflict)
	}
	if err := assignIdentity(&lib.ID, &lib.CreatedAt, &lib.UpdatedAt); err != nil {
		return lib, err
	}
	err := d.inTx(ctx, func(tx *sql.Tx) error {
		if lib.Default {
			if _, err := tx.ExecContext(ctx, `UPDATE libraries SET is_default=0 WHERE is_default=1`); err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO libraries (`+libraryColumns+`) VALUES (?,?,?,'',?,?,?)`,
			lib.ID, lib.Name, lib.Description, lib.Default, timestamp(lib.CreatedAt), timestamp(lib.UpdatedAt))
		return conflict(err, "a library named %q", lib.Name)
	})
	if err != nil {
		return lib, fmt.Errorf("create library: %w", err)
	}
	return lib, nil
}

func (d *DB) GetLibrary(ctx context.Context, idOrName string) (domain.Library, error) {
	q, args := `SELECT `+libraryColumns+` FROM libraries WHERE id=? OR name=?`, []any{idOrName, idOrName}
	if idOrName == "" {
		q, args, idOrName = `SELECT `+libraryColumns+` FROM libraries WHERE is_default=1`, nil, "default"
	}
	lib, err := scanLibrary(d.db.QueryRowContext(ctx, q, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return lib, fmt.Errorf("library %q: %w", idOrName, store.ErrNotFound)
	}
	return lib, err
}

func (d *DB) ListLibraries(ctx context.Context) ([]domain.Library, error) {
	rows, err := d.db.QueryContext(ctx, `SELECT `+libraryColumns+` FROM libraries ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Library
	for rows.Next() {
		lib, err := scanLibrary(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, lib)
	}
	return out, rows.Err()
}

func (d *DB) UpdateLibrary(ctx context.Context, lib domain.Library) (domain.Library, error) {
	if err := domain.ValidateLibrary(lib); err != nil {
		return lib, err
	}
	lib.UpdatedAt = time.Now().UTC()
	err := d.inTx(ctx, func(tx *sql.Tx) error {
		var kind string
		var wasDefault bool
		err := tx.QueryRowContext(ctx, `SELECT kind, is_default FROM libraries WHERE id=?`, lib.ID).Scan(&kind, &wasDefault)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("library %q: %w", lib.ID, store.ErrNotFound)
		}
		if err != nil {
			return err
		}
		switch {
		case lib.Default && kind == sharedKind:
			return domain.Invalidf("the shared library can't be the default library")
		case !lib.Default && wasDefault:
			return domain.Invalidf("there is always a default library; make another library the default instead")
		case lib.Default && !wasDefault:
			if _, err := tx.ExecContext(ctx, `UPDATE libraries SET is_default=0 WHERE is_default=1`); err != nil {
				return err
			}
		}
		_, err = tx.ExecContext(ctx, `UPDATE libraries SET name=?,description=?,is_default=?,updated_at=? WHERE id=?`,
			lib.Name, lib.Description, lib.Default, timestamp(lib.UpdatedAt), lib.ID)
		return conflict(err, "a library named %q", lib.Name)
	})
	if err != nil {
		return lib, fmt.Errorf("update library: %w", err)
	}
	return lib, nil
}

func (d *DB) DeleteLibrary(ctx context.Context, id string) error {
	var isDefault bool
	if err := d.db.QueryRowContext(ctx, `SELECT is_default FROM libraries WHERE id=?`, id).Scan(&isDefault); err == nil && isDefault {
		return fmt.Errorf("the default library can't be deleted; make another library the default first: %w", store.ErrConflict)
	}
	res, err := d.db.ExecContext(ctx, `DELETE FROM libraries WHERE id=?`, id)
	if err != nil {
		return fmt.Errorf("delete library: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("library %q: %w", id, store.ErrNotFound)
	}
	return nil
}

type scanner interface{ Scan(...any) error }

func scanLibrary(r scanner) (domain.Library, error) {
	var lib domain.Library
	var created, updated, kind string
	if err := r.Scan(&lib.ID, &lib.Name, &lib.Description, &kind, &lib.Default, &created, &updated); err != nil {
		return lib, err
	}
	lib.Shared = kind == sharedKind
	lib.CreatedAt, lib.UpdatedAt = parseTime(created), parseTime(updated)
	return lib, nil
}

// Library returns the library with the given ID.
func (d *DB) Library(id string) store.Library { return &library{db: d.db, q: d.db, id: id} }
