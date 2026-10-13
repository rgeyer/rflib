package store

import (
	"context"
	"errors"
	"path/filepath"
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
CREATE TABLE channels (receive_ctcss_hz REAL, transmit_ctcss_hz REAL);
INSERT INTO channels(receive_ctcss_hz, transmit_ctcss_hz) VALUES (69.3, 100.0);`)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	var receive, transmit int64
	if err := s.db.QueryRowContext(ctx, `SELECT receive_ctcss_millihz, transmit_ctcss_millihz FROM channels`).Scan(&receive, &transmit); err != nil {
		t.Fatal(err)
	}
	if receive != 69300 || transmit != 100000 {
		t.Fatalf("migrated values = %d, %d", receive, transmit)
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
