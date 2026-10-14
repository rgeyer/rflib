package app

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ryan/rflib/internal/domain"
	"github.com/ryan/rflib/internal/query"
	"github.com/ryan/rflib/internal/store"
	"github.com/ryan/rflib/internal/store/sqlite"
)

var none = domain.Tone{Kind: domain.ToneNone}

func newService(t *testing.T) (*Service, store.Store) {
	t.Helper()
	ctx := context.Background()
	d, err := sqlite.Open(filepath.Join(t.TempDir(), "lib.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := d.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	s, err := Open(ctx, d, domain.DefaultLibrary)
	if err != nil {
		t.Fatal(err)
	}
	return s, d
}

func add(t *testing.T, s *Service, name string, mhz float64, svc domain.Service) domain.Channel {
	t.Helper()
	c, _, err := s.CreateChannel(context.Background(), domain.Channel{Name: name, ReceiveHz: int64(mhz * 1e6), Mode: "FM", Service: svc, ReceiveTone: none, TransmitTone: none})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func names(cs []domain.Channel) string {
	var out []string
	for _, c := range cs {
		out = append(out, c.Name)
	}
	return strings.Join(out, ",")
}

func TestOpenUnknownLibrary(t *testing.T) {
	_, d := newService(t)
	if _, err := Open(context.Background(), d, "nope"); !errors.Is(err, store.ErrNotFound) || !strings.Contains(err.Error(), `no library named "nope"`) {
		t.Fatalf("Open = %v", err)
	}
}

func TestCreateChannelReturnsBandPlanWarnings(t *testing.T) {
	s, _ := newService(t)
	tx := int64(465_275_000)
	_, warnings, err := s.CreateChannel(context.Background(), domain.Channel{Name: "Sheriff", ReceiveHz: 460_275_000, TransmitHz: &tx, Mode: "FM", ReceiveTone: none, TransmitTone: none})
	if err != nil || len(warnings) != 1 || !strings.Contains(warnings[0], "receive-only") {
		t.Fatalf("warnings = %v, %v", warnings, err)
	}
}

func TestQueriesResolveInTheLibrary(t *testing.T) {
	ctx := context.Background()
	s, _ := newService(t)
	add(t, s, "Calling", 146.52, domain.ServiceAmateur)
	add(t, s, "CB 19", 27.185, domain.ServiceCB)
	r, err := s.CreateRadio(ctx, domain.Radio{Name: "HT", Model: "radioddity-uv5g"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateRadio(ctx, domain.Radio{Name: "X", Model: "nope"}); err == nil {
		t.Error("unknown model accepted")
	}
	if _, res, err := s.AddChannels(ctx, "HT", AddRequest{IDs: nil, Query: mustParse(t, "freq:>1")}); err != nil || res.Added != 1 || names(res.Skipped) != "CB 19" {
		t.Fatalf("add = %+v, %v", res, err)
	}
	for q, want := range map[string]string{
		"radio:HT":                 "Calling",
		"radio:" + r.ID:            "Calling",
		"radio:none":               "CB 19",
		"tunes:HT":                 "Calling",
		"tunes:yaesu-ft857":        "CB 19,Calling",
		"-tunes:radioddity-uv5g":   "CB 19",
		"service:cb OR service:ns": "",
	} {
		cs, err := s.Find(ctx, q)
		if want == "" {
			if err == nil {
				t.Errorf("%q: no error", q)
			}
			continue
		}
		if err != nil || names(cs) != want {
			t.Errorf("%q = %s, %v; want %s", q, names(cs), err, want)
		}
	}
}

func TestSavedSearchRules(t *testing.T) {
	ctx := context.Background()
	s, _ := newService(t)
	wx, err := s.CreateSearch(ctx, domain.SavedSearch{Name: "@WX", Query: "band:noaa"})
	if err != nil || wx.Name != "wx" {
		t.Fatalf("create = %+v, %v", wx, err)
	}
	if _, err := s.CreateSearch(ctx, domain.SavedSearch{Name: "quiet", Query: "@wx OR band:frs"}); err != nil {
		t.Fatal(err)
	}
	wx.Query = "@quiet"
	if _, err := s.UpdateSearch(ctx, wx); err == nil || !strings.Contains(err.Error(), "uses itself (@wx → @quiet → @wx)") {
		t.Errorf("cycle: %v", err)
	}
	wx.Query, wx.Name = "band:noaa", "weather"
	if _, err := s.UpdateSearch(ctx, wx); err == nil || !strings.Contains(err.Error(), "used by @quiet") {
		t.Errorf("rename while used: %v", err)
	}
	if err := s.DeleteSearch(ctx, "wx"); err == nil {
		t.Error("deleted a search in use")
	}
	if xs, err := s.ListSearches(ctx); err != nil || len(xs) != 2 || xs[0].Matches != 0 || xs[0].Err != nil {
		t.Errorf("list = %+v, %v", xs, err)
	}
}

func TestSyncPlacesAndClaims(t *testing.T) {
	ctx := context.Background()
	s, _ := newService(t)
	add(t, s, "WX1", 162.55, domain.ServiceNOAA)
	add(t, s, "WX2", 162.40, domain.ServiceNOAA)
	add(t, s, "Calling", 146.52, domain.ServiceAmateur)
	add(t, s, "CB 19", 27.185, domain.ServiceCB)
	if _, err := s.CreateRadio(ctx, domain.Radio{Name: "HT", Model: "radioddity-uv5g"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Sync(ctx, "HT", false, false); err == nil || !strings.Contains(err.Error(), "no sources") {
		t.Fatalf("sync without sources: %v", err)
	}
	if _, _, err := s.AddSource(ctx, "HT", domain.RadioSource{Query: "band:noaa OR service:amateur", Sort: "freq"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AddSource(ctx, "HT", domain.RadioSource{Query: "frq:1"}); err == nil {
		t.Error("bad query accepted")
	}
	if _, _, err := s.AddSource(ctx, "HT", domain.RadioSource{Query: "service:noaa OR service:cb"}); err != nil {
		t.Fatal(err)
	}
	dry, err := s.Sync(ctx, "HT", true, false)
	if err != nil {
		t.Fatal(err)
	}
	// The first source claims the weather channels; the second gets none
	// of them, and CB 19 is out of band.
	if names(dry.Sources[0].Add) != "Calling,WX2,WX1" || names(dry.Sources[1].Add) != "" || names(dry.Sources[1].Skipped) != "CB 19" || dry.Added() != 0 {
		t.Fatalf("dry run = %+v", dry)
	}
	if _, es, _ := s.RadioEntries(ctx, "HT"); len(es) != 0 {
		t.Fatal("dry run changed the radio")
	}
	res, err := s.Sync(ctx, "HT", false, false)
	if err != nil || res.Added() != 3 || res.PlanErr != nil {
		t.Fatalf("sync = %+v, %v", res, err)
	}
	if again, _ := s.Sync(ctx, "HT", false, false); again.Added() != 0 {
		t.Errorf("second sync added %d", again.Added())
	}
	if err := s.MoveChannel(ctx, "HT", "missing", Move{Top: true}); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("move missing: %v", err)
	}
}

func mustParse(t *testing.T, text string) query.Node {
	t.Helper()
	n, err := query.Parse(text)
	if err != nil {
		t.Fatal(err)
	}
	return n
}
