package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ryan/rflib/internal/domain"
	"github.com/ryan/rflib/internal/store"
	"github.com/ryan/rflib/internal/store/storetest"
)

func newTestDB(t *testing.T) *DB {
	t.Helper()
	d, err := Open(filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := d.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestContract(t *testing.T) {
	storetest.Run(t, func(t *testing.T) store.Store { return newTestDB(t) })
}

func TestMigrateIsIdempotentAndReady(t *testing.T) {
	ctx := context.Background()
	d := newTestDB(t)
	if err := d.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := d.Ready(ctx); err != nil {
		t.Fatal(err)
	}
}

// openAt creates a database at a schema version by running the migrations
// up to it, then runs seed.
func openAt(t *testing.T, version int64, seed string) *DB {
	t.Helper()
	ctx := context.Background()
	d, err := Open(filepath.Join(t.TempDir(), "old.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	stmts := []string{`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY)`, baselineSchema}
	for _, m := range migrations {
		if m.version > baselineVersion && m.version <= version {
			stmts = append(stmts, m.sql)
		}
	}
	stmts = append(stmts, `INSERT INTO schema_migrations(version) VALUES (?)`, seed)
	for i, q := range stmts {
		args := []any{}
		if i == len(stmts)-2 {
			args = append(args, version)
		}
		if _, err := d.db.ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	return d
}

// A v1 database: CTCSS in Hz, power as text.
func TestMigrateV1(t *testing.T) {
	ctx := context.Background()
	d, err := Open(filepath.Join(t.TempDir(), "v1.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	_, err = d.db.ExecContext(ctx, `
CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY);
INSERT INTO schema_migrations(version) VALUES (1);
CREATE TABLE groups (id TEXT PRIMARY KEY, name TEXT NOT NULL UNIQUE, description TEXT NOT NULL DEFAULT '', latitude REAL, longitude REAL, radius_km REAL, notes TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL);
CREATE TABLE channels (id TEXT PRIMARY KEY, group_id TEXT REFERENCES groups(id) ON DELETE SET NULL, name TEXT NOT NULL, receive_hz INTEGER NOT NULL, transmit_hz INTEGER, mode TEXT NOT NULL, receive_tone_kind TEXT NOT NULL, receive_ctcss_hz REAL, receive_dcs_code INTEGER, receive_dcs_inverted INTEGER NOT NULL DEFAULT 0, transmit_tone_kind TEXT NOT NULL, transmit_ctcss_hz REAL, transmit_dcs_code INTEGER, transmit_dcs_inverted INTEGER NOT NULL DEFAULT 0, tone_squelch INTEGER NOT NULL DEFAULT 0, power TEXT NOT NULL DEFAULT '', bandwidth TEXT NOT NULL DEFAULT '', skip INTEGER NOT NULL DEFAULT 0, notes TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL);
CREATE TABLE channel_tags (channel_id TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE, tag TEXT NOT NULL, PRIMARY KEY(channel_id, tag));
INSERT INTO channels (id, name, receive_hz, mode, receive_tone_kind, receive_ctcss_hz, transmit_tone_kind, transmit_ctcss_hz, power, created_at, updated_at) VALUES
	('a', 'A', 146520000, 'FM', 'ctcss', 69.3, 'ctcss', 100.0, 'HIGH', '', ''),
	('b', 'B', 146520000, 'FM', 'none', NULL, 'none', NULL, '50W', '', '');`)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	lib, err := d.GetLibrary(ctx, domain.DefaultLibrary)
	if err != nil {
		t.Fatal(err)
	}
	cs, err := d.Library(lib.ID).ListChannels(ctx)
	if err != nil || len(cs) != 2 {
		t.Fatalf("channels = %+v, %v", cs, err)
	}
	if *cs[0].ReceiveTone.CTCSSMilliHz != 69300 || *cs[0].TransmitTone.CTCSSMilliHz != 100000 {
		t.Errorf("tones = %+v", cs[0])
	}
	if cs[0].Power != domain.PowerHigh || cs[1].Power != domain.PowerUnset {
		t.Errorf("power = %q, %q; want level names mapped and watts dropped", cs[0].Power, cs[1].Power)
	}
}

// A v5 radio's sync filter becomes its first source.
func TestMigrateV6SyncFilterToSource(t *testing.T) {
	ctx := context.Background()
	d := openAt(t, 5, `
INSERT INTO groups (id, name, created_at, updated_at) VALUES ('g1', 'Metro', '', '');
INSERT INTO radios (id, name, model, sync_tag, sync_group_id, sync_service, created_at, updated_at) VALUES
	('r1', 'HT', 'baofeng-uv5rm', 'my "ham"', 'g1', 'amateur', '', ''),
	('r2', 'Truck', 'yaesu-ftm150', '', NULL, '', '', '');`)
	if err := d.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	lib, _ := d.GetLibrary(ctx, domain.DefaultLibrary)
	l := d.Library(lib.ID)
	ht, err := l.RadioSources(ctx, "r1")
	if err != nil {
		t.Fatal(err)
	}
	if len(ht) != 1 || ht[0].Query != `tag:"my \"ham\"" group:g1 service:amateur` || ht[0].Sort != "name" || ht[0].Position != 0 || len(ht[0].ID) != 36 {
		t.Fatalf("HT sources = %+v", ht)
	}
	if truck, _ := l.RadioSources(ctx, "r2"); len(truck) != 0 {
		t.Fatalf("Truck had no filter but got sources %+v", truck)
	}
}

// A v6 database moves whole into the default library, references intact.
func TestMigrateV7Libraries(t *testing.T) {
	ctx := context.Background()
	d := openAt(t, 6, `
INSERT INTO groups (id, name, power, created_at, updated_at) VALUES ('g1', 'Metro', 'low', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z');
INSERT INTO channels (id, group_id, name, receive_hz, mode, service, receive_tone_kind, transmit_tone_kind, created_at, updated_at) VALUES
	('c1', 'g1', 'Calling', 146520000, 'FM', 'amateur', 'none', 'none', '', ''),
	('c2', NULL, 'Other', 146550000, 'FM', '', 'none', 'none', '', '');
INSERT INTO channel_tags VALUES ('c1', 'ham');
INSERT INTO channel_short_names VALUES ('c1', 0, 'Call');
INSERT INTO radios (id, name, model, sync_tag, created_at, updated_at) VALUES ('r1', 'HT', 'baofeng-uv5rm', 'ham', '', '');
INSERT INTO radio_sources (id, radio_id, position, query, sort) VALUES ('s1', 'r1', 0, 'tag:ham', 'freq');
INSERT INTO radio_channels (radio_id, channel_id, position, source_id, name) VALUES ('r1', 'c1', 0, 's1', 'Kids'), ('r1', 'c2', 1, NULL, '');
INSERT INTO searches (id, name, query, created_at, updated_at) VALUES ('q1', 'wx', 'band:noaa', '', '');`)
	if err := d.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := d.Ready(ctx); err != nil {
		t.Fatal(err)
	}
	var fk int
	if err := d.db.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&fk); err != nil || fk != 1 {
		t.Fatalf("foreign keys left off after the rebuild: %d, %v", fk, err)
	}
	lib, err := d.GetLibrary(ctx, domain.DefaultLibrary)
	if err != nil {
		t.Fatal(err)
	}
	l := d.Library(lib.ID)
	c, err := l.GetChannel(ctx, "c1")
	if err != nil {
		t.Fatal(err)
	}
	if c.Group == nil || c.Group.Name != "Metro" || c.Group.Power != domain.PowerLow || len(c.Tags) != 1 || len(c.ShortNames) != 1 || c.Service != domain.ServiceAmateur {
		t.Errorf("channel = %+v", c)
	}
	es, err := l.RadioEntries(ctx, "r1")
	if err != nil || len(es) != 2 || es[0].SourceID != "s1" || es[0].Name != "Kids" {
		t.Fatalf("entries = %+v, %v", es, err)
	}
	if x, err := l.GetSearch(ctx, "wx"); err != nil || x.Query != "band:noaa" {
		t.Errorf("search = %+v, %v", x, err)
	}
	// Cascades survive the rebuild.
	if err := l.DeleteChannel(ctx, "c1"); err != nil {
		t.Fatal(err)
	}
	if es, _ := l.RadioEntries(ctx, "r1"); len(es) != 1 {
		t.Errorf("channel delete didn't reach the radio: %+v", es)
	}
	if err := l.DeleteGroup(ctx, "g1"); err != nil {
		t.Fatal(err)
	}
	var orphans int
	if err := d.db.QueryRowContext(ctx, `SELECT count(*) FROM channel_tags`).Scan(&orphans); err != nil || orphans != 0 {
		t.Errorf("orphaned tags: %d, %v", orphans, err)
	}
	if err := d.DeleteLibrary(ctx, lib.ID); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"channels", "radios", "radio_channels", "radio_sources", "searches"} {
		var n int
		if err := d.db.QueryRowContext(ctx, `SELECT count(*) FROM `+table).Scan(&n); err != nil || n != 0 {
			t.Errorf("%s after deleting the library: %d, %v", table, n, err)
		}
	}
}
