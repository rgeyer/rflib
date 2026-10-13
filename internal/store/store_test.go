package store

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ryan/rflib/internal/domain"
)

func TestChannelCRUDAndFilters(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	lat, lon := 45.52, -122.68
	g, err := s.CreateGroup(ctx, domain.Group{Name: "Portland", Latitude: &lat, Longitude: &lon})
	if err != nil {
		t.Fatal(err)
	}
	tone, _ := domain.ParseTone("ctcss:100.0")
	tx := int64(146340000)
	c, err := s.CreateChannel(ctx, domain.Channel{Name: "Metro", GroupID: &g.ID, ReceiveHz: 146940000, TransmitHz: &tx, Mode: "fm", ReceiveTone: tone, TransmitTone: tone, ToneSquelch: true, Tags: []string{"HAM", "local"}})
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := s.GetChannel(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Mode != "FM" || loaded.Group == nil || len(loaded.Tags) != 2 || loaded.ReceiveTone.CTCSSMilliHz == nil || *loaded.ReceiveTone.CTCSSMilliHz != 100000 {
		t.Fatalf("loaded channel = %#v", loaded)
	}
	nearby, err := s.ListChannels(ctx, ChannelFilter{Tag: "ham", Latitude: &lat, Longitude: &lon, RadiusKM: floatPtr(10)})
	if err != nil || len(nearby) != 1 {
		t.Fatalf("nearby = %d, %v", len(nearby), err)
	}
	if err := s.DeleteChannel(ctx, c.ID); err != nil {
		t.Fatal(err)
	}
}
func TestMigrateV1CTCSSValues(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "legacy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_, err = s.db.ExecContext(ctx, `
CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY);
INSERT INTO schema_migrations(version) VALUES (1);
CREATE TABLE groups (id TEXT PRIMARY KEY);
CREATE TABLE channels (id TEXT PRIMARY KEY, power TEXT NOT NULL DEFAULT '', receive_ctcss_hz REAL, transmit_ctcss_hz REAL);
INSERT INTO channels(id, power, receive_ctcss_hz, transmit_ctcss_hz) VALUES ('a', 'HIGH', 69.3, 100.0), ('b', '50W', 0, 0);`)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	var receive, transmit int64
	if err := s.db.QueryRowContext(ctx, `SELECT receive_ctcss_millihz, transmit_ctcss_millihz FROM channels WHERE id='a'`).Scan(&receive, &transmit); err != nil {
		t.Fatal(err)
	}
	if receive != 69300 || transmit != 100000 {
		t.Fatalf("migrated values = %d, %d", receive, transmit)
	}
	var powers string
	if err := s.db.QueryRowContext(ctx, `SELECT group_concat(power, ',' ORDER BY id) FROM channels`).Scan(&powers); err != nil {
		t.Fatal(err)
	}
	if powers != "high," {
		t.Fatalf("migrated power = %q, want level names mapped and watts dropped", powers)
	}
}

func floatPtr(v float64) *float64 { return &v }

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestForeignKeysEnforced(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	g, err := s.CreateGroup(ctx, domain.Group{Name: "Metro"})
	if err != nil {
		t.Fatal(err)
	}
	none := domain.Tone{Kind: domain.ToneNone}
	c, err := s.CreateChannel(ctx, domain.Channel{Name: "Simplex", GroupID: &g.ID, ReceiveHz: 146520000, Mode: "FM", ReceiveTone: none, TransmitTone: none, Tags: []string{"b", "a"}})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetChannel(ctx, c.ID); len(got.Tags) != 2 || got.Tags[0] != "a" {
		t.Fatalf("tags = %v, want sorted [a b]", got.Tags)
	}

	if err := s.DeleteGroup(ctx, "Metro"); err != nil {
		t.Fatalf("delete group by name: %v", err)
	}
	got, err := s.GetChannel(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.GroupID != nil || got.Group != nil {
		t.Fatalf("group delete did not null channel group: %#v", got.GroupID)
	}

	if err := s.DeleteChannel(ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	var orphans int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM channel_tags`).Scan(&orphans); err != nil {
		t.Fatal(err)
	}
	if orphans != 0 {
		t.Fatalf("channel delete left %d orphaned tags", orphans)
	}

	missing := "no-such-group"
	if _, err := s.CreateChannel(ctx, domain.Channel{Name: "Bad", GroupID: &missing, ReceiveHz: 146520000, Mode: "FM", ReceiveTone: none, TransmitTone: none}); err == nil {
		t.Fatal("expected foreign key violation for unknown group")
	}
}

func TestNotFound(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	if _, err := s.GetGroup(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetGroup err = %v", err)
	}
	if _, err := s.GetChannel(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetChannel err = %v", err)
	}
	if err := s.DeleteChannel(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("DeleteChannel err = %v", err)
	}
	none := domain.Tone{Kind: domain.ToneNone}
	if _, err := s.UpdateChannel(ctx, domain.Channel{ID: "nope", Name: "x", ReceiveHz: 1, Mode: "FM", ReceiveTone: none, TransmitTone: none}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("UpdateChannel err = %v", err)
	}
}

func TestMigrateIsIdempotentAndReady(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.Ready(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestShortNamesKeepOrderAndCascade(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	none := domain.Tone{Kind: domain.ToneNone}
	c, err := s.CreateChannel(ctx, domain.Channel{Name: "Metro Repeater", ReceiveHz: 146940000, Mode: "FM", ReceiveTone: none, TransmitTone: none,
		ShortNames: []string{"Metro Rpt", " MtRpt ", "metro rpt", "Metro Repeater", ""}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.GetChannel(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got.ShortNames, "|") != "Metro Rpt|MtRpt" {
		t.Fatalf("short names = %q", got.ShortNames)
	}
	got.ShortNames = []string{"Z", "A"}
	if _, err := s.UpdateChannel(ctx, got); err != nil {
		t.Fatal(err)
	}
	if again, _ := s.GetChannel(ctx, c.ID); strings.Join(again.ShortNames, "|") != "Z|A" {
		t.Fatalf("updated short names = %q", again.ShortNames)
	}
	if err := s.DeleteChannel(ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	var left int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM channel_short_names`).Scan(&left); err != nil || left != 0 {
		t.Fatalf("orphaned short names: %d, %v", left, err)
	}
}

func TestRadioChannelsOrderAndOverrides(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	none := domain.Tone{Kind: domain.ToneNone}
	var ids []string
	for i, name := range []string{"A", "B", "C", "D"} {
		c, err := s.CreateChannel(ctx, domain.Channel{Name: name, ReceiveHz: int64(146_520_000 + i*15_000), Mode: "FM", ReceiveTone: none, TransmitTone: none})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, c.ID)
	}
	r, err := s.CreateRadio(ctx, domain.Radio{Name: "Truck", Model: "yaesu-ftm150", DefaultPower: domain.PowerMedium})
	if err != nil {
		t.Fatal(err)
	}
	order := func() string {
		t.Helper()
		es, err := s.RadioEntries(ctx, r.ID)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for i, e := range es {
			if e.Position != i {
				t.Fatalf("positions not contiguous: %+v", es)
			}
			names = append(names, e.Channel.Name)
		}
		return strings.Join(names, "")
	}

	if n, err := s.AddRadioChannels(ctx, r.ID, []string{ids[2], ids[0], ids[1]}); err != nil || n != 3 {
		t.Fatalf("add: %d, %v", n, err)
	}
	if n, _ := s.AddRadioChannels(ctx, r.ID, []string{ids[0], ids[3]}); n != 1 || order() != "CABD" {
		t.Fatalf("append skips existing: %d, %s", n, order())
	}
	if err := s.MoveRadioChannel(ctx, r.ID, ids[3], 0); err != nil || order() != "DCAB" {
		t.Fatalf("move to top: %v, %s", err, order())
	}
	if err := s.MoveRadioChannel(ctx, r.ID, ids[3], 99); err != nil || order() != "CABD" {
		t.Fatalf("move past end: %v, %s", err, order())
	}

	fifty, skip := 50, true
	if err := s.SetRadioEntry(ctx, r.ID, domain.RadioEntry{Channel: domain.Channel{ID: ids[1]}, SectionStart: &fifty, Name: " Bee ", Power: domain.PowerLow, Skip: &skip}); err != nil {
		t.Fatal(err)
	}
	es, _ := s.RadioEntries(ctx, r.ID)
	b := es[2]
	if b.Channel.Name != "B" || b.SectionStart == nil || *b.SectionStart != 50 || b.Name != "Bee" || b.Power != domain.PowerLow || b.Skip == nil || !*b.Skip {
		t.Fatalf("overrides = %+v", b)
	}
	if es[0].Skip != nil || es[0].SectionStart != nil {
		t.Fatalf("unset overrides should stay nil: %+v", es[0])
	}

	// Deleting a channel removes it from the radio; the list still reads in order.
	if err := s.DeleteChannel(ctx, ids[0]); err != nil {
		t.Fatal(err)
	}
	if order() != "CBD" {
		t.Fatalf("after channel delete: %s", order())
	}
	if n, _ := s.RemoveRadioChannels(ctx, r.ID, []string{ids[2], "missing"}); n != 1 || order() != "BD" {
		t.Fatalf("remove: %d, %s", n, order())
	}
	if err := s.MoveRadioChannel(ctx, r.ID, ids[0], 0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("moving a channel not on the radio: %v", err)
	}
	if err := s.DeleteRadio(ctx, "Truck"); err != nil {
		t.Fatal(err)
	}
	var left int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM radio_channels`).Scan(&left); err != nil || left != 0 {
		t.Fatalf("radio delete left %d entries: %v", left, err)
	}
}

// A v5 radio's sync filter becomes its first source.
func TestMigrateV6SyncFilterToSource(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "v5.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	stmts := []string{`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY)`, baselineSchema}
	for _, m := range migrations {
		if m.version > baselineVersion && m.version <= 5 {
			stmts = append(stmts, m.sql)
		}
	}
	stmts = append(stmts, `INSERT INTO schema_migrations(version) VALUES (5);
INSERT INTO groups (id, name, created_at, updated_at) VALUES ('g1', 'Metro', '', '');
INSERT INTO radios (id, name, model, sync_tag, sync_group_id, sync_service, created_at, updated_at) VALUES
	('r1', 'HT', 'baofeng-uv5rm', 'my "ham"', 'g1', 'amateur', '', ''),
	('r2', 'Truck', 'yaesu-ftm150', '', NULL, '', '', '');`)
	for _, q := range stmts {
		if _, err := s.db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	ht, err := s.RadioSources(ctx, "r1")
	if err != nil {
		t.Fatal(err)
	}
	if len(ht) != 1 || ht[0].Query != `tag:"my \"ham\"" group:g1 service:amateur` || ht[0].Sort != "name" || ht[0].Position != 0 || len(ht[0].ID) != 36 {
		t.Fatalf("HT sources = %+v", ht)
	}
	if truck, _ := s.RadioSources(ctx, "r2"); len(truck) != 0 {
		t.Fatalf("Truck had no filter but got sources %+v", truck)
	}
}

func TestSavedSearchCRUD(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	x, err := s.CreateSearch(ctx, domain.SavedSearch{Name: " Local ", Query: "tag:local"})
	if err != nil {
		t.Fatal(err)
	}
	if x.Name != "local" {
		t.Fatalf("name = %q", x.Name)
	}
	if _, err := s.CreateSearch(ctx, domain.SavedSearch{Name: "local", Query: "tag:x"}); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("duplicate: %v", err)
	}
	for _, ref := range []string{"local", "@local", "LOCAL", x.ID} {
		if got, err := s.GetSearch(ctx, ref); err != nil || got.ID != x.ID {
			t.Fatalf("GetSearch(%q) = %v, %v", ref, got, err)
		}
	}
	x.Query = "tag:home"
	if _, err := s.UpdateSearch(ctx, x); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteSearch(ctx, x.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetSearch(ctx, "local"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after delete: %v", err)
	}
}
