package app

import (
	"context"
	"errors"
	"fmt"
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

// Personal tags that repeat a channel's fields are allowed but warned
// about, once: when they're added.
func TestTagRepeatWarnings(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()
	cb := func(name string, hz int64) domain.Channel {
		return domain.Channel{Name: name, ReceiveHz: hz, TransmitHz: &hz, Mode: "AM", Service: domain.ServiceCB, ReceiveTone: none, TransmitTone: none, Tags: []string{"cb", "mobile"}}
	}
	c, warnings, err := s.CreateChannel(ctx, cb("CB 19", 27_185_000))
	if err != nil || len(warnings) != 1 || !strings.Contains(warnings[0], `tag "cb" repeats one of the channel's fields: service:cb`) {
		t.Fatalf("create warnings = %v, %v", warnings, err)
	}
	c.Notes = "trucker channel"
	if _, warnings, err = s.UpdateChannel(ctx, c); err != nil || len(warnings) != 0 {
		t.Fatalf("a kept tag warned again: %v, %v", warnings, err)
	}
	other, _, err := s.CreateChannel(ctx, domain.Channel{Name: "CB 9", ReceiveHz: 27_065_000, Mode: "AM", Service: domain.ServiceCB, ReceiveTone: none, TransmitTone: none})
	if err != nil {
		t.Fatal(err)
	}
	_, warnings, err = s.UpdateChannels(ctx, Selection{IDs: []string{c.ID, other.ID}}, ChannelEdit{Tags: &TagEdit{Add: []string{"am", "cb"}}})
	if err != nil || len(warnings) != 2 || !strings.HasSuffix(warnings[0], "(on 2 channels)") || strings.HasSuffix(warnings[1], "channels)") {
		t.Fatalf("bulk warnings = %q, %v", warnings, err)
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
	if err != nil || res.Added() != 3 || res.LayoutErr != nil {
		t.Fatalf("sync = %+v, %v", res, err)
	}
	if again, _ := s.Sync(ctx, "HT", SyncOptions{}); again.Added() != 0 {
		t.Errorf("second sync added %d", again.Added())
	}
	if err := s.MoveChannel(ctx, "HT", "missing", Move{Top: true}); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("move missing: %v", err)
	}
}

// A section sorted by name keeps its channels in order, whatever order
// they were added in; placing one by hand makes it manual, in the order it
// had; and a change that would run sections together is refused.
func TestSortedSections(t *testing.T) {
	ctx := context.Background()
	s, _ := newService(t)
	for _, n := range []string{"GMRS 15", "GMRS 2", "GMRS 1", "GMRS 10"} {
		add(t, s, n, 462.55, domain.ServiceGMRS)
	}
	mine := add(t, s, "Mine", 146.52, domain.ServiceAmateur)
	if _, err := s.CreateRadio(ctx, domain.Radio{Name: "HT", Model: "baofeng-uv5rm"}); err != nil {
		t.Fatal(err)
	}
	twenty, name := 20, "name"
	gmrs, err := s.AddSection(ctx, "HT", domain.RadioSection{Name: "GMRS", Start: &twenty})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AddSource(ctx, "HT", domain.RadioSource{Query: "service:gmrs", Sort: "freq", SectionID: gmrs.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Sync(ctx, "HT", SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AddChannels(ctx, "HT", AddRequest{IDs: []string{mine.ID}}); err != nil {
		t.Fatal(err)
	}
	memories := func() string {
		t.Helper()
		l, _, err := s.Layout(ctx, "HT")
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, m := range l.Memories {
			out = append(out, fmt.Sprintf("%d %s", m.Location, m.Original.Name))
		}
		return strings.Join(out, ", ")
	}
	// Out of order, as placed by hand.
	_, es, _ := s.RadioEntries(ctx, "HT")
	if err := s.lib.ReorderRadioChannels(ctx, es[0].Section.RadioID, []string{es[3].Channel.ID, es[1].Channel.ID, es[0].Channel.ID, es[2].Channel.ID, es[4].Channel.ID}); err != nil {
		t.Fatal(err)
	}
	if got := memories(); got != "20 GMRS 15, 21 GMRS 2, 22 GMRS 1, 23 GMRS 10, 24 Mine" {
		t.Fatalf("as placed = %s", got)
	}
	if _, err := s.UpdateSection(ctx, "HT", "gmrs", SectionEdit{Sort: &name}); err != nil {
		t.Fatal(err)
	}
	if got := memories(); got != "20 GMRS 1, 21 GMRS 2, 22 GMRS 10, 23 GMRS 15, 24 Mine" {
		t.Fatalf("sorted = %s", got)
	}
	_, es, _ = s.RadioEntries(ctx, "HT")
	if err := s.MoveChannel(ctx, "HT", es[3].Channel.ID, Move{Top: true}); err != nil {
		t.Fatal(err)
	}
	if got := memories(); got != "20 GMRS 15, 21 GMRS 1, 22 GMRS 2, 23 GMRS 10, 24 Mine" {
		t.Errorf("moved = %s", got)
	}
	if sec, _ := s.Section(ctx, "HT", "GMRS"); sec.Sort != "" {
		t.Errorf("the section is still sorted by %s", sec.Sort)
	}
	// Into another section, by name.
	if err := s.MoveChannel(ctx, "HT", mine.ID, Move{Section: "gmrs", To: 2}); err != nil {
		t.Fatal(err)
	}
	if got := memories(); got != "20 GMRS 15, 21 Mine, 22 GMRS 1, 23 GMRS 2, 24 GMRS 10" {
		t.Errorf("into GMRS = %s", got)
	}
	five := 5
	if _, err := s.AddSection(ctx, "HT", domain.RadioSection{Name: "Early", Start: &five}); err == nil || !strings.Contains(err.Error(), "already reach memory 24") {
		t.Errorf("a section behind the others: %v", err)
	}
	if _, secs, _ := s.Sections(ctx, "HT"); len(secs) != 2 {
		t.Errorf("the refused section stayed: %+v", secs)
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

	// A built-in list goes into the shared list only; a private channel
	// that matches one becomes a link, keeping its own name as a short name.
	mine := add(t, s, "GMRS 15 Rpt", 462.55, domain.ServiceGMRS)
	simplex := mine.ReceiveHz
	mine.TransmitHz, mine.Tags, mine.Notes = &simplex, []string{"family"}, "our channel"
	if _, _, err := s.UpdateChannel(ctx, mine); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateSeed(ctx, gmrs, false); !errors.Is(err, errNotShared) {
		t.Errorf("update from a private library: %v", err)
	}
	if st, err := sh.UpdateSeed(ctx, gmrs, false); err != nil || len(st.Changes) != 30 || !st.Installed {
		t.Fatalf("seed = %+v, %v", st, err)
	}
	if mineNow, _ := s.GetChannel(ctx, mine.ID); mineNow.Link != nil {
		t.Fatal("seeding touched a private library")
	}
	cvs, err := s.ConvertToLinks(ctx, nil, nil, false)
	if err != nil || len(cvs) != 1 {
		t.Fatalf("convert = %+v, %v", cvs, err)
	}
	// The private channel gives way to the shared one, which carries its
	// tags, notes, and name as the user's annotation.
	if _, err := s.GetChannel(ctx, mine.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("the private channel stayed: %v", err)
	}
	link, _ := s.GetChannel(ctx, cvs[0].Shared.ID)
	if link.Link == nil || link.ID != cvs[0].Shared.ID || link.Name != "GMRS 15" || strings.Join(link.ShortNames, ",") != "GMRS 15 Rpt" || strings.Join(link.Tags, ",") != "family" || link.Notes != "our channel" {
		t.Fatalf("converted = %+v", link)
	}
	if again, err := sh.UpdateSeed(ctx, gmrs, false); err != nil || again.Pending() {
		t.Errorf("update again = %+v, %v", again.Changes, err)
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
	if _, _, err := s.UpdateChannels(ctx, Selection{IDs: []string{link.ID}}, ChannelEdit{Power: &low}); err == nil || !strings.Contains(err.Error(), "set on each radio") {
		t.Errorf("power on a shared channel: %v", err)
	}
	if _, _, err := s.UpdateChannels(ctx, Selection{IDs: []string{link.ID}}, ChannelEdit{Tags: &TagEdit{Add: []string{"x"}}}); err != nil {
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
	// A radio takes shared channels by query too, without linking.
	if _, res, err := s.AddChannels(ctx, "HT", AddRequest{Query: mustParse(t, `in:shared name:"GMRS 17"`)}); err != nil || res.Added != 2 {
		t.Errorf("in:shared onto a radio = %+v, %v", res, err)
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
	moved, err := s.Relink(ctx, Selection{IDs: []string{link.ID}})
	if err != nil || len(moved) != 1 || moved[0].Name != "GMRS 16" || moved[0].Changed() || !slices.Contains(moved[0].Tags, "family") {
		t.Fatalf("relink = %+v, %v", moved, err)
	}
	if es, _, _ := s.radioEntriesNamed(ctx, "HT"); !slices.Contains(es, "GMRS 16") || slices.Contains(es, "GMRS 15") {
		t.Errorf("HT after relink = %v", es)
	}
	if _, revs, err := sh.History(ctx, link.Link.SharedID); err != nil || len(revs) != 3 || revs[2].Reason != "moved" {
		t.Errorf("history = %+v, %v", revs, err)
	}

	// A copy stops following the shared channel, and takes its place on
	// radios.
	copies, err := s.CopyChannels(ctx, Selection{IDs: []string{moved[0].ID}})
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
	s, d := newService(t)
	frs := add(t, s, "FRS 1", 462.5625, domain.ServiceFRS)
	hz := frs.ReceiveHz
	frs.TransmitHz = &hz
	if _, _, err := s.UpdateChannel(ctx, frs); err != nil {
		t.Fatal(err)
	}
	sh := openShared(t, d)
	gmrs, _ := seed.Get("us-gmrs")
	if _, err := sh.UpdateSeed(ctx, gmrs, false); err != nil {
		t.Fatal(err)
	}
	if cvs, err := s.ConvertToLinks(ctx, nil, nil, true); err != nil || len(cvs) != 0 {
		t.Fatalf("gmrs = %d converted, %v", len(cvs), err)
	}
	frsList, _ := seed.Get("us-frs")
	if _, err := sh.UpdateSeed(ctx, frsList, false); err != nil {
		t.Fatal(err)
	}
	if cvs, err := s.ConvertToLinks(ctx, nil, nil, false); err != nil || len(cvs) != 1 || cvs[0].Shared.Name != "FRS 1" {
		t.Fatalf("frs = %+v, %v", cvs, err)
	}
}

// sharedChannel adds a channel to the shared library.
func sharedChannel(t *testing.T, sh *Service, name string, mhz float64) domain.Channel {
	t.Helper()
	c, err := sh.lib.CreateChannel(context.Background(), domain.Channel{Name: name, ReceiveHz: int64(mhz * 1e6), Mode: "FM", Service: domain.ServiceAmateur, ReceiveTone: none, TransmitTone: none})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestPlanRowsAndPlanSources(t *testing.T) {
	ctx := context.Background()
	s, d := newService(t)
	sh := openShared(t, d)
	call := sharedChannel(t, sh, "2m Call", 146.52)
	a, b := sharedChannel(t, sh, "Broadcast Pk", 146.79), sharedChannel(t, sh, "La Cumbre", 145.18)
	sharedChannel(t, sh, "Airband", 121.5) // outside the radio's bands
	if _, err := s.CreateSystem(ctx, domain.System{Name: "Hub", Members: []domain.SystemMember{{ChannelID: a.ID}, {ChannelID: b.ID}}}, "test"); err != nil {
		t.Fatal(err)
	}
	ten := 10
	p, err := s.CreatePlan(ctx, domain.Plan{Name: "Fair", Status: domain.PlanPublished, Entries: []domain.PlanEntry{
		{ChannelID: call.ID, Name: "FAIR TAC1", Assignment: "Tactical"},
		{Query: "system:hub", SectionStart: &ten, Assignment: "Wide area"},
	}}, "test")
	if err != nil {
		t.Fatal(err)
	}
	rows, err := s.PlanRows(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range rows {
		got = append(got, fmt.Sprintf("%d %s %s", r.Number, r.Name, r.Assignment))
	}
	if strings.Join(got, "; ") != "1 FAIR TAC1 Tactical; 10 Broadcast Pk Wide area; 11 La Cumbre Wide area" {
		t.Errorf("rows = %q", got)
	}
	// A section start behind the entries before it, or a channel twice,
	// is refused.
	bad := p
	bad.Entries = append(slices.Clone(p.Entries), domain.PlanEntry{ChannelID: call.ID})
	if _, err := s.UpdatePlan(ctx, bad, "test"); err == nil {
		t.Error("a channel twice was accepted")
	}
	five := 5
	bad.Entries = []domain.PlanEntry{{ChannelID: a.ID, SectionStart: &ten}, {ChannelID: b.ID, SectionStart: &five}}
	if _, err := s.UpdatePlan(ctx, bad, "test"); err == nil {
		t.Error("a backwards section start was accepted")
	}

	// A radio with a query source and the plan: the plan claims its
	// channels, links them in, names them, and numbers them from 100.
	add(t, s, "Simplex", 146.55, domain.ServiceAmateur)
	if _, err := s.CreateRadio(ctx, domain.Radio{Name: "HT", Model: "baofeng-uv5rm"}); err != nil {
		t.Fatal(err)
	}
	// The query source matches the plan's channels too, but the plan
	// claims them first.
	if _, _, err := s.AddSource(ctx, "HT", domain.RadioSource{Query: "freq:146.5..146.6"}); err != nil {
		t.Fatal(err)
	}
	hundred := 100
	fair, err := s.AddSection(ctx, "HT", domain.RadioSection{Name: "Fair", Start: &hundred})
	if err != nil {
		t.Fatal(err)
	}
	_, planSource, err := s.AddSource(ctx, "HT", domain.RadioSource{PlanID: "fair", SectionID: fair.ID, PlanNumbers: domain.PlanNumbersMatch})
	if err != nil {
		t.Fatal(err)
	}
	dry, err := s.Sync(ctx, "HT", SyncOptions{DryRun: true})
	if err != nil || len(dry.Sources[1].Add) != 3 {
		t.Fatalf("dry run = %+v, %v", dry, err)
	}
	if _, err := s.Sync(ctx, "HT", SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	memories := func() string {
		t.Helper()
		l, _, err := s.Layout(ctx, "HT")
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, m := range l.Memories {
			out = append(out, fmt.Sprintf("%d %s", m.Location, m.Channel.Name))
		}
		return strings.Join(out, ", ")
	}
	if got := memories(); got != "1 Simplex, 100 FAIR TAC1, 109 Broadcast Pk, 110 La Cumbre" {
		t.Errorf("memories = %q", got)
	}
	// Compact: the plan's order, without its gaps.
	planSource.PlanNumbers = domain.PlanNumbersCompact
	if err := s.lib.UpdateRadioSource(ctx, planSource); err != nil {
		t.Fatal(err)
	}
	if got := memories(); got != "1 Simplex, 100 FAIR TAC1, 101 Broadcast Pk, 102 La Cumbre" {
		t.Errorf("compact = %q", got)
	}
	planSource.PlanNumbers = domain.PlanNumbersMatch
	if err := s.lib.UpdateRadioSource(ctx, planSource); err != nil {
		t.Fatal(err)
	}
	// The plan changes: its channels follow, and a dropped one comes off.
	p, _ = s.GetPlan(ctx, "fair")
	p.Entries = []domain.PlanEntry{p.Entries[1], {ChannelID: call.ID, Name: "FAIR TAC9"}}
	p.Entries[0].Query = "name:broadcast"
	if _, err := s.UpdatePlan(ctx, p, "test"); err != nil {
		t.Fatal(err)
	}
	res, err := s.Sync(ctx, "HT", SyncOptions{})
	if err != nil || len(res.Sources[1].Removed) != 1 {
		t.Fatalf("sync = %+v, %v", res, err)
	}
	if got := memories(); got != "1 Simplex, 109 Broadcast Pk, 110 FAIR TAC9" {
		t.Errorf("after the change = %q", got)
	}
	// Archived, it stays as it is; removed, its channels go.
	p.Status = domain.PlanArchived
	if _, err := s.UpdatePlan(ctx, p, "test"); err != nil {
		t.Fatal(err)
	}
	if res, err := s.Sync(ctx, "HT", SyncOptions{}); err != nil || res.Sources[1].Note == "" || memories() != "1 Simplex, 109 Broadcast Pk, 110 FAIR TAC9" {
		t.Errorf("archived sync = %+v, %v", res.Sources[1], err)
	}
	if n, err := s.RemoveSource(ctx, "HT", 2, false); err != nil || n != 2 || memories() != "1 Simplex" {
		t.Errorf("remove = %d, %v; memories %q", n, err, memories())
	}
}

func TestInSharedAlone(t *testing.T) {
	s, d := newService(t)
	sharedChannel(t, openShared(t, d), "2m Call", 146.52)
	if cs, err := s.Find(context.Background(), "in:shared"); err != nil || len(cs) != 1 {
		t.Errorf("in:shared = %v, %v", names(cs), err)
	}
}

// radioEntriesNamed lists a radio's channel names, for tests.
func (s *Service) radioEntriesNamed(ctx context.Context, radio string) ([]string, domain.Radio, error) {
	r, es, err := s.RadioEntries(ctx, radio)
	var out []string
	for _, e := range es {
		out = append(out, e.Channel.Name)
	}
	return out, r, err
}

