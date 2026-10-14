package app

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ryan/rflib/internal/domain"
	"github.com/ryan/rflib/internal/query"
	"github.com/ryan/rflib/internal/seed"
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
	if _, err := s.Sync(ctx, "HT", SyncOptions{}); err == nil || !strings.Contains(err.Error(), "no sources") {
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
	dry, err := s.Sync(ctx, "HT", SyncOptions{DryRun: true})
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
	res, err := s.Sync(ctx, "HT", SyncOptions{})
	if err != nil || res.Added() != 3 || res.PlanErr != nil {
		t.Fatalf("sync = %+v, %v", res, err)
	}
	if again, _ := s.Sync(ctx, "HT", SyncOptions{}); again.Added() != 0 {
		t.Errorf("second sync added %d", again.Added())
	}
	if err := s.MoveChannel(ctx, "HT", "missing", Move{Top: true}); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("move missing: %v", err)
	}
}

func TestSyncResorts(t *testing.T) {
	ctx := context.Background()
	s, _ := newService(t)
	for _, n := range []string{"GMRS 15", "GMRS 2", "GMRS 1", "GMRS 10"} {
		add(t, s, n, 462.55, domain.ServiceGMRS)
	}
	mine := add(t, s, "Mine", 146.52, domain.ServiceAmateur)
	if _, err := s.CreateRadio(ctx, domain.Radio{Name: "HT", Model: "baofeng-uv5rm"}); err != nil {
		t.Fatal(err)
	}
	twenty := 20
	_, src, err := s.AddSource(ctx, "HT", domain.RadioSource{Query: "service:gmrs", Sort: "name", SectionStart: &twenty})
	if err != nil {
		t.Fatal(err)
	}
	// Put the source's channels out of order, as an older sort left them,
	// with a hand-added channel among them.
	if _, err := s.Sync(ctx, "HT", SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	_, es, _ := s.RadioEntries(ctx, "HT")
	order := []string{es[3].Channel.ID, es[2].Channel.ID, es[1].Channel.ID, es[0].Channel.ID}
	if err := s.lib.ReorderRadioChannels(ctx, src.RadioID, order); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AddChannels(ctx, "HT", AddRequest{IDs: []string{mine.ID}}); err != nil {
		t.Fatal(err)
	}
	if err := s.lib.ReorderRadioChannels(ctx, src.RadioID, []string{order[0], order[1], mine.ID, order[2], order[3]}); err != nil {
		t.Fatal(err)
	}
	listed := func() string {
		_, es, _ := s.RadioEntries(ctx, "HT")
		var out []string
		for _, e := range es {
			out = append(out, e.Channel.Name)
		}
		return strings.Join(out, ",")
	}
	before := listed()
	if dry, err := s.Sync(ctx, "HT", SyncOptions{DryRun: true, Resort: true}); err != nil || dry.Resorted() != 4 || listed() != before {
		t.Fatalf("dry run: %d moved, %v; list %s", dry.Resorted(), err, listed())
	}
	if plain, _ := s.Sync(ctx, "HT", SyncOptions{}); plain.Resorted() != 0 {
		t.Errorf("a plain sync re-sorted %d", plain.Resorted())
	}
	res, err := s.Sync(ctx, "HT", SyncOptions{Resort: true})
	if err != nil || res.Resorted() != 4 || res.PlanErr != nil {
		t.Fatalf("resort: %+v, %v", res, err)
	}
	if got := listed(); got != "GMRS 1,GMRS 2,Mine,GMRS 10,GMRS 15" {
		t.Errorf("after resort: %s", got)
	}
	_, es, _ = s.RadioEntries(ctx, "HT")
	if es[0].SectionStart == nil || *es[0].SectionStart != 20 {
		t.Errorf("section start left the first slot: %+v", es[0])
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

func TestChannelIDPrefixes(t *testing.T) {
	ctx := context.Background()
	s, _ := newService(t)
	for _, id := range []string{"abcd1111", "abcd2222", "ffff0000"} {
		if _, _, err := s.CreateChannel(ctx, domain.Channel{ID: id, Name: id, ReceiveHz: 146_520_000, Mode: "FM", ReceiveTone: none, TransmitTone: none}); err != nil {
			t.Fatal(err)
		}
	}
	for ref, want := range map[string]string{"abcd1111": "abcd1111", "abcd1": "abcd1111", "ffff": "ffff0000", "abcd": "ambiguous", "fff": "not found", "zzzz": "not found"} {
		got, err := s.ChannelID(ctx, ref)
		if err != nil {
			got = err.Error()
		}
		if !strings.Contains(got, want) {
			t.Errorf("ChannelID(%q) = %q, want %q", ref, got, want)
		}
	}
}

func TestBulkSelectUpdateDelete(t *testing.T) {
	ctx := context.Background()
	s, _ := newService(t)
	wx1 := add(t, s, "WX1", 162.55, domain.ServiceNOAA)
	wx2 := add(t, s, "WX2", 162.40, domain.ServiceNOAA)
	call := add(t, s, "Calling", 146.52, domain.ServiceAmateur)
	if _, err := s.Select(ctx, Selection{}); err == nil {
		t.Error("an empty selection selected")
	}
	// IDs and a query: a channel must be in both.
	cs, err := s.Select(ctx, Selection{IDs: []string{wx1.ID, call.ID}, Query: mustParse(t, "band:noaa")})
	if err != nil || names(cs) != "WX1" {
		t.Fatalf("select = %s, %v", names(cs), err)
	}
	if all, _ := s.Select(ctx, Selection{All: true}); len(all) != 3 {
		t.Errorf("all = %s", names(all))
	}

	low, gmrs := domain.PowerLow, domain.ServiceGMRS
	edit := ChannelEdit{Tags: &TagEdit{Add: []string{"Weather", "wx"}}, Power: &low}
	if _, _, err := s.UpdateChannels(ctx, Selection{Query: mustParse(t, "band:noaa")}, edit); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.UpdateChannels(ctx, Selection{IDs: []string{wx2.ID}}, ChannelEdit{Tags: &TagEdit{Remove: []string{"wx"}}}); err != nil {
		t.Fatal(err)
	}
	got1, _ := s.GetChannel(ctx, wx1.ID)
	got2, _ := s.GetChannel(ctx, wx2.ID)
	if strings.Join(got1.Tags, ",") != "weather,wx" || strings.Join(got2.Tags, ",") != "weather" || got1.Power != low {
		t.Errorf("after edits: %v %v %s", got1.Tags, got2.Tags, got1.Power)
	}
	// Band-plan warnings name their channel.
	_, warnings, err := s.UpdateChannels(ctx, Selection{IDs: []string{call.ID}}, ChannelEdit{Service: &gmrs})
	if err != nil || len(warnings) == 0 || !strings.HasPrefix(warnings[0], "Calling: ") {
		t.Errorf("warnings = %v, %v", warnings, err)
	}

	if _, err := s.CreateRadio(ctx, domain.Radio{Name: "HT", Model: "baofeng-uv5rm"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AddChannels(ctx, "HT", AddRequest{IDs: []string{wx1.ID, call.ID}}); err != nil {
		t.Fatal(err)
	}
	if _, on, _ := s.OnRadio(ctx, "HT", Selection{All: true}); names(on) != "Calling,WX1" {
		t.Errorf("on radio = %s", names(on))
	}
	high := domain.PowerHigh
	if cs, err := s.SetEntries(ctx, "HT", Selection{Query: mustParse(t, "tag:weather")}, EntryEdit{SetPower: true, Power: high}); err != nil || names(cs) != "WX1" {
		t.Errorf("set entries = %s, %v", names(cs), err)
	}
	if _, e, _ := s.RadioEntry(ctx, "HT", wx1.ID[:8]); e.Power != high {
		t.Errorf("override = %+v", e)
	}
	gone, err := s.DeleteChannels(ctx, Selection{Query: mustParse(t, "tag:weather")})
	if err != nil || names(gone) != "WX1,WX2" {
		t.Fatalf("delete = %s, %v", names(gone), err)
	}
	if _, on, _ := s.OnRadio(ctx, "HT", Selection{All: true}); names(on) != "Calling" {
		t.Errorf("deleted channel still on the radio: %s", names(on))
	}
	if _, removed, err := s.RemoveChannels(ctx, "HT", Selection{All: true}); err != nil || names(removed) != "Calling" {
		t.Errorf("remove all = %s, %v", names(removed), err)
	}
}

func openShared(t *testing.T, d store.Store) *Service {
	t.Helper()
	sh, err := Open(context.Background(), d, domain.SharedLibrary)
	if err != nil {
		t.Fatal(err)
	}
	return sh
}

func TestSharedChannelsLinkCopyAndChanges(t *testing.T) {
	ctx := context.Background()
	s, d := newService(t)
	sh := openShared(t, d)
	gmrs, _ := seed.Get("us-gmrs")

	// A seeded private channel from before becomes a link, keeping its own
	// name as a short name; the rest are linked new.
	mine := add(t, s, "GMRS 15 Rpt", 462.55, domain.ServiceGMRS)
	simplex := mine.ReceiveHz
	mine.TransmitHz, mine.Tags, mine.Notes = &simplex, []string{"family"}, "our channel"
	if _, _, err := s.UpdateChannel(ctx, mine); err != nil {
		t.Fatal(err)
	}
	dry, err := s.InstallSeed(ctx, gmrs, nil, true)
	if err != nil || len(dry.Converted) != 1 || len(dry.Linked) != 29 {
		t.Fatalf("dry run = %d converted, %d linked, %v", len(dry.Converted), len(dry.Linked), err)
	}
	if all, _ := sh.ListChannels(ctx); len(all) != 0 {
		t.Fatal("dry run filled the shared library")
	}
	res, err := s.InstallSeed(ctx, gmrs, []string{"seeded"}, false)
	if err != nil || res.SharedAdded != 30 || len(res.Converted) != 1 || len(res.Linked) != 29 {
		t.Fatalf("install = %+v, %v", res, err)
	}
	link, _ := s.GetChannel(ctx, mine.ID)
	if link.Link == nil || link.Name != "GMRS 15" || strings.Join(link.ShortNames, ",") != "GMRS 15 Rpt" || strings.Join(link.Tags, ",") != "family" || link.Notes != "our channel" {
		t.Fatalf("converted = %+v", link)
	}
	if again, _ := s.InstallSeed(ctx, gmrs, nil, false); again.SharedAdded != 0 || again.SharedUpdated != 0 || len(again.Linked) != 0 || len(again.Already) != 30 {
		t.Errorf("reinstall = %+v", again)
	}
	// Seed tags that repeat fields are left off shared channels.
	if cs, _ := sh.Find(ctx, "tag:gmrs"); len(cs) != 0 {
		t.Errorf("shared channels kept field tags: %s", names(cs))
	}

	// The shared library is read-only to everyday changes, and a link's
	// shared fields belong to it.
	if _, _, err := sh.CreateChannel(ctx, domain.Channel{Name: "x", ReceiveHz: 1, Mode: "FM", ReceiveTone: none, TransmitTone: none}); !errors.Is(err, ErrReadOnly) {
		t.Errorf("shared create: %v", err)
	}
	edited := link
	edited.ReceiveHz = 462_575_000
	if _, _, err := s.UpdateChannel(ctx, edited); err == nil || !strings.Contains(err.Error(), "rx can't change here") {
		t.Errorf("edit shared field: %v", err)
	}
	gmrsSvc := domain.ServiceOther
	if _, _, err := s.UpdateChannels(ctx, Selection{IDs: []string{link.ID}}, ChannelEdit{Service: &gmrsSvc}); err == nil {
		t.Error("bulk edit of a shared field")
	}
	low := domain.PowerLow
	if _, _, err := s.UpdateChannels(ctx, Selection{IDs: []string{link.ID}}, ChannelEdit{Power: &low, Tags: &TagEdit{Add: []string{"x"}}}); err != nil {
		t.Errorf("own fields: %v", err)
	}

	// in:shared searches the whole list; links come into the library.
	if cs, err := s.Find(ctx, "in:shared freq:462.5625"); err != nil || names(cs) != "GMRS 1" {
		t.Errorf("in:shared = %s, %v", names(cs), err)
	}
	if _, err := s.Find(ctx, "-in:shared"); err == nil {
		t.Error("negated in: accepted")
	}

	// The maintainer takes the repeater off the air: the link follows, and
	// shows in Changes until acknowledged.
	back := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	if _, err := sh.SetStatus(ctx, link.Link.SharedID, StatusChange{Status: domain.StatusOffAir, Note: "antenna work", BackOn: &back}, "reported by owner"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetStatus(ctx, link.ID, StatusChange{Status: domain.StatusActive}, ""); !errors.Is(err, errNotShared) {
		t.Errorf("status from a private library: %v", err)
	}
	if _, err := s.CreateRadio(ctx, domain.Radio{Name: "HT", Model: "baofeng-uv5rm"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AddChannels(ctx, "HT", AddRequest{IDs: []string{link.ID}}); err != nil {
		t.Fatal(err)
	}
	changes, err := s.Changes(ctx)
	if err != nil || len(changes) != 1 || strings.Join(changes[0].Reasons, ",") != "reported by owner" || strings.Join(changes[0].Radios, ",") != "HT" ||
		!slices.Contains(changes[0].Changes, "status: active → off-air") {
		t.Fatalf("changes = %+v, %v", changes, err)
	}
	if _, _, err := s.AddChannels(ctx, "HT", AddRequest{Query: mustParse(t, "in:shared name:x")}); err == nil {
		t.Error("in:shared added to a radio")
	}
	if acked, err := s.Acknowledge(ctx, Selection{All: true}); err != nil || len(acked) != 1 {
		t.Fatalf("acknowledge = %d, %v", len(acked), err)
	}
	if changes, _ := s.Changes(ctx); len(changes) != 0 {
		t.Errorf("changes after acknowledging = %+v", changes)
	}

	// Retired with a replacement: relinking moves the link over.
	other, _ := sh.Find(ctx, `name:"GMRS 16"`)
	if _, err := sh.SetStatus(ctx, link.Link.SharedID, StatusChange{Status: domain.StatusRetired, ReplacedBy: other[0].ID[:8]}, "moved"); err != nil {
		t.Fatal(err)
	}
	if changes, _ := s.Changes(ctx); len(changes) != 1 || changes[0].Replacement == nil || changes[0].Replacement.Name != "GMRS 16" {
		t.Fatalf("retired change = %+v", changes)
	}
	if moved, err := s.Relink(ctx, Selection{IDs: []string{link.ID}}); err != nil || len(moved) != 1 || moved[0].Name != "GMRS 16" || moved[0].Changed() {
		t.Fatalf("relink = %+v, %v", moved, err)
	}
	if _, revs, err := sh.History(ctx, link.Link.SharedID); err != nil || len(revs) != 3 || revs[2].Reason != "moved" {
		t.Errorf("history = %+v, %v", revs, err)
	}

	// A copy stops following the shared channel.
	copies, err := s.CopyChannels(ctx, Selection{IDs: []string{link.ID}})
	if err != nil || len(copies) != 1 || copies[0].Link != nil || copies[0].CopiedFrom == "" || !slices.Contains(copies[0].Tags, "family") {
		t.Fatalf("copy = %+v, %v", copies, err)
	}
	copied := copies[0]
	copied.ReceiveHz = 462_600_000
	if _, _, err := s.UpdateChannel(ctx, copied); err != nil {
		t.Errorf("edit a copy: %v", err)
	}
}

// FRS and GMRS share frequencies; converting goes by service too.
func TestConvertMatchesService(t *testing.T) {
	ctx := context.Background()
	s, _ := newService(t)
	frs := add(t, s, "FRS 1", 462.5625, domain.ServiceFRS)
	hz := frs.ReceiveHz
	frs.TransmitHz = &hz
	if _, _, err := s.UpdateChannel(ctx, frs); err != nil {
		t.Fatal(err)
	}
	gmrs, _ := seed.Get("us-gmrs")
	if res, err := s.InstallSeed(ctx, gmrs, nil, false); err != nil || len(res.Converted) != 0 || len(res.Linked) != 30 {
		t.Fatalf("gmrs = %d converted, %v", len(res.Converted), err)
	}
	frsList, _ := seed.Get("us-frs")
	if res, err := s.InstallSeed(ctx, frsList, nil, false); err != nil || len(res.Converted) != 1 || res.Converted[0].Shared.Name != "FRS 1" {
		t.Fatalf("frs = %+v, %v", res.Converted, err)
	}
}
