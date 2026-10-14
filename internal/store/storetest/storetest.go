// Package storetest is the contract every store.Store implementation must
// pass. A backend's tests call Run with a function that returns a new,
// migrated, empty store.
package storetest

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ryan/rflib/internal/domain"
	"github.com/ryan/rflib/internal/store"
)

// Run runs the contract against stores from newStore.
func Run(t *testing.T, newStore func(t *testing.T) store.Store) {
	tests := []struct {
		name string
		fn   func(*testing.T, store.Store)
	}{
		{"DefaultLibrary", testDefaultLibrary},
		{"LibraryCRUD", testLibraryCRUD},
		{"ChannelRoundTrip", testChannelRoundTrip},
		{"GroupDeleteUngroups", testGroupDeleteUngroups},
		{"NotFound", testNotFound},
		{"NamesUniquePerLibrary", testNamesUniquePerLibrary},
		{"ShortNamesKeepOrder", testShortNamesKeepOrder},
		{"RadioOrderAndOverrides", testRadioOrderAndOverrides},
		{"SourcePlacement", testSourcePlacement},
		{"SavedSearches", testSavedSearches},
		{"LibraryIsolation", testLibraryIsolation},
		{"DeleteLibraryCascades", testDeleteLibraryCascades},
		{"SuppliedIDs", testSuppliedIDs},
		{"TransactionRollsBack", testTransactionRollsBack},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) { tc.fn(t, newStore(t)) })
	}
}

var none = domain.Tone{Kind: domain.ToneNone}

func ctx() context.Context { return context.Background() }

// newLibrary creates a library and returns it.
func newLibrary(t *testing.T, s store.Store, name string) store.Library {
	t.Helper()
	lib, err := s.CreateLibrary(ctx(), domain.Library{Name: name})
	if err != nil {
		t.Fatal(err)
	}
	return s.Library(lib.ID)
}

func channel(t *testing.T, l store.Library, name string, hz int64) domain.Channel {
	t.Helper()
	c, err := l.CreateChannel(ctx(), domain.Channel{Name: name, ReceiveHz: hz, Mode: "FM", ReceiveTone: none, TransmitTone: none})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func mustNotFound(t *testing.T, what string, err error) {
	t.Helper()
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("%s: err = %v, want not found", what, err)
	}
}

func testDefaultLibrary(t *testing.T, s store.Store) {
	lib, err := s.GetLibrary(ctx(), domain.DefaultLibrary)
	if err != nil {
		t.Fatalf("a new store has no %q library: %v", domain.DefaultLibrary, err)
	}
	if libs, _ := s.ListLibraries(ctx()); len(libs) != 1 || libs[0].ID != lib.ID {
		t.Fatalf("libraries = %+v", libs)
	}
}

func testLibraryCRUD(t *testing.T, s store.Store) {
	lib, err := s.CreateLibrary(ctx(), domain.Library{Name: "Club", Description: "club radios"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateLibrary(ctx(), domain.Library{Name: "Club"}); !errors.Is(err, store.ErrConflict) {
		t.Errorf("duplicate library: %v", err)
	}
	if _, err := s.CreateLibrary(ctx(), domain.Library{Name: " "}); err == nil {
		t.Error("blank name accepted")
	}
	for _, ref := range []string{"Club", lib.ID} {
		if got, err := s.GetLibrary(ctx(), ref); err != nil || got.Description != "club radios" {
			t.Errorf("GetLibrary(%q) = %+v, %v", ref, got, err)
		}
	}
	lib.Name = "Club Radios"
	if _, err := s.UpdateLibrary(ctx(), lib); err != nil {
		t.Fatal(err)
	}
	lib.Name = domain.DefaultLibrary
	if _, err := s.UpdateLibrary(ctx(), lib); !errors.Is(err, store.ErrConflict) {
		t.Errorf("rename onto another library: %v", err)
	}
	if err := s.DeleteLibrary(ctx(), lib.ID); err != nil {
		t.Fatal(err)
	}
	_, err = s.GetLibrary(ctx(), "Club Radios")
	mustNotFound(t, "deleted library", err)
	mustNotFound(t, "delete again", s.DeleteLibrary(ctx(), lib.ID))
}

func testChannelRoundTrip(t *testing.T, s store.Store) {
	l := newLibrary(t, s, "L")
	lat, lon, radius := 45.52, -122.68, 30.0
	g, err := l.CreateGroup(ctx(), domain.Group{Name: "Portland", Latitude: &lat, Longitude: &lon, RadiusKM: &radius, Power: domain.PowerLow})
	if err != nil {
		t.Fatal(err)
	}
	tone, _ := domain.ParseTone("ctcss:100.0")
	dcs, _ := domain.ParseTone("dcs:023:inverted")
	tx := int64(146_340_000)
	c, err := l.CreateChannel(ctx(), domain.Channel{Name: "Metro", GroupID: &g.ID, ReceiveHz: 146_940_000, TransmitHz: &tx, Mode: "fm",
		Service: domain.ServiceAmateur, ReceiveTone: dcs, TransmitTone: tone, ToneSquelch: true, Power: domain.PowerHigh, Skip: true,
		Notes: "n", Tags: []string{"HAM", "local", "ham"}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := l.GetChannel(ctx(), c.ID)
	if err != nil {
		t.Fatal(err)
	}
	switch {
	case got.Mode != "FM", got.Service != domain.ServiceAmateur, got.Power != domain.PowerHigh, !got.Skip, !got.ToneSquelch, got.Notes != "n":
		t.Errorf("fields = %+v", got)
	case got.TransmitHz == nil || *got.TransmitHz != tx:
		t.Errorf("transmit = %v", got.TransmitHz)
	case domain.FormatTone(got.ReceiveTone) != "dcs:023:inverted", domain.FormatTone(got.TransmitTone) != "ctcss:100.000":
		t.Errorf("tones = %s, %s", domain.FormatTone(got.ReceiveTone), domain.FormatTone(got.TransmitTone))
	case strings.Join(got.Tags, ",") != "ham,local":
		t.Errorf("tags = %v, want normalized and sorted", got.Tags)
	case got.Group == nil || got.Group.Name != "Portland" || got.Group.RadiusKM == nil || got.Group.Power != domain.PowerLow:
		t.Errorf("group = %+v", got.Group)
	case got.CreatedAt.IsZero() || got.UpdatedAt.IsZero():
		t.Error("timestamps not set")
	}
	got.Name, got.TransmitHz = "Metro 2", nil
	if _, err := l.UpdateChannel(ctx(), got); err != nil {
		t.Fatal(err)
	}
	all, err := l.ListChannels(ctx())
	if err != nil || len(all) != 1 || all[0].Name != "Metro 2" || all[0].TransmitHz != nil {
		t.Fatalf("after update: %+v, %v", all, err)
	}
	channel(t, l, "Alpha", 146_520_000)
	if all, _ := l.ListChannels(ctx()); len(all) != 2 || all[0].Name != "Alpha" {
		t.Errorf("list not in name order: %+v", all)
	}
	if err := l.DeleteChannel(ctx(), c.ID); err != nil {
		t.Fatal(err)
	}
}

func testGroupDeleteUngroups(t *testing.T, s store.Store) {
	l := newLibrary(t, s, "L")
	g, err := l.CreateGroup(ctx(), domain.Group{Name: "Metro"})
	if err != nil {
		t.Fatal(err)
	}
	c, err := l.CreateChannel(ctx(), domain.Channel{Name: "Simplex", GroupID: &g.ID, ReceiveHz: 146_520_000, Mode: "FM", ReceiveTone: none, TransmitTone: none})
	if err != nil {
		t.Fatal(err)
	}
	if err := l.DeleteGroup(ctx(), "Metro"); err != nil {
		t.Fatalf("delete group by name: %v", err)
	}
	got, err := l.GetChannel(ctx(), c.ID)
	if err != nil || got.GroupID != nil || got.Group != nil {
		t.Fatalf("channel after group delete: %+v, %v", got, err)
	}
	missing := "no-such-group"
	_, err = l.CreateChannel(ctx(), domain.Channel{Name: "Bad", GroupID: &missing, ReceiveHz: 146_520_000, Mode: "FM", ReceiveTone: none, TransmitTone: none})
	mustNotFound(t, "channel in a missing group", err)
}

func testNotFound(t *testing.T, s store.Store) {
	l := newLibrary(t, s, "L")
	_, err := l.GetGroup(ctx(), "nope")
	mustNotFound(t, "GetGroup", err)
	_, err = l.UpdateGroup(ctx(), domain.Group{ID: "nope", Name: "x"})
	mustNotFound(t, "UpdateGroup", err)
	mustNotFound(t, "DeleteGroup", l.DeleteGroup(ctx(), "nope"))
	_, err = l.GetChannel(ctx(), "nope")
	mustNotFound(t, "GetChannel", err)
	_, err = l.UpdateChannel(ctx(), domain.Channel{ID: "nope", Name: "x", ReceiveHz: 1, Mode: "FM", ReceiveTone: none, TransmitTone: none})
	mustNotFound(t, "UpdateChannel", err)
	mustNotFound(t, "DeleteChannel", l.DeleteChannel(ctx(), "nope"))
	_, err = l.GetRadio(ctx(), "nope")
	mustNotFound(t, "GetRadio", err)
	mustNotFound(t, "DeleteRadio", l.DeleteRadio(ctx(), "nope"))
	_, err = l.RadioEntries(ctx(), "nope")
	mustNotFound(t, "RadioEntries", err)
	_, err = l.RadioSources(ctx(), "nope")
	mustNotFound(t, "RadioSources", err)
	_, err = l.GetSearch(ctx(), "nope")
	mustNotFound(t, "GetSearch", err)
	mustNotFound(t, "DeleteSearch", l.DeleteSearch(ctx(), "nope"))
}

func testNamesUniquePerLibrary(t *testing.T, s store.Store) {
	a, b := newLibrary(t, s, "A"), newLibrary(t, s, "B")
	for _, l := range []store.Library{a, b} {
		if _, err := l.CreateGroup(ctx(), domain.Group{Name: "Home"}); err != nil {
			t.Fatal(err)
		}
		if _, err := l.CreateRadio(ctx(), domain.Radio{Name: "HT", Model: "baofeng-uv5rm"}); err != nil {
			t.Fatal(err)
		}
		if _, err := l.CreateSearch(ctx(), domain.SavedSearch{Name: "wx", Query: "band:noaa"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := a.CreateGroup(ctx(), domain.Group{Name: "Home"}); !errors.Is(err, store.ErrConflict) {
		t.Errorf("duplicate group: %v", err)
	}
	if _, err := a.CreateRadio(ctx(), domain.Radio{Name: "HT", Model: "baofeng-uv5rm"}); !errors.Is(err, store.ErrConflict) {
		t.Errorf("duplicate radio: %v", err)
	}
	if _, err := a.CreateSearch(ctx(), domain.SavedSearch{Name: "wx", Query: "x"}); !errors.Is(err, store.ErrConflict) {
		t.Errorf("duplicate search: %v", err)
	}
	g, err := a.CreateGroup(ctx(), domain.Group{Name: "Away"})
	if err != nil {
		t.Fatal(err)
	}
	g.Name = "Home"
	if _, err := a.UpdateGroup(ctx(), g); !errors.Is(err, store.ErrConflict) {
		t.Errorf("rename group onto another: %v", err)
	}
}

func testShortNamesKeepOrder(t *testing.T, s store.Store) {
	l := newLibrary(t, s, "L")
	c, err := l.CreateChannel(ctx(), domain.Channel{Name: "Metro Repeater", ReceiveHz: 146_940_000, Mode: "FM", ReceiveTone: none, TransmitTone: none,
		ShortNames: []string{"Metro Rpt", " MtRpt ", "metro rpt", "Metro Repeater", ""}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := l.GetChannel(ctx(), c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got.ShortNames, "|") != "Metro Rpt|MtRpt" {
		t.Fatalf("short names = %q", got.ShortNames)
	}
	got.ShortNames = []string{"Z", "A"}
	if _, err := l.UpdateChannel(ctx(), got); err != nil {
		t.Fatal(err)
	}
	if again, _ := l.GetChannel(ctx(), c.ID); strings.Join(again.ShortNames, "|") != "Z|A" {
		t.Fatalf("updated short names = %q", again.ShortNames)
	}
}

func radioOrder(t *testing.T, l store.Library, radioID string) string {
	t.Helper()
	es, err := l.RadioEntries(ctx(), radioID)
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

func testRadioOrderAndOverrides(t *testing.T, s store.Store) {
	l := newLibrary(t, s, "L")
	var ids []string
	for i, name := range []string{"A", "B", "C", "D"} {
		ids = append(ids, channel(t, l, name, int64(146_520_000+i*15_000)).ID)
	}
	r, err := l.CreateRadio(ctx(), domain.Radio{Name: "Truck", Model: "yaesu-ftm150", DefaultPower: domain.PowerMedium})
	if err != nil {
		t.Fatal(err)
	}
	order := func() string { return radioOrder(t, l, r.ID) }
	if n, err := l.AddRadioChannels(ctx(), r.ID, []string{ids[2], ids[0], ids[1]}); err != nil || n != 3 {
		t.Fatalf("add: %d, %v", n, err)
	}
	if n, _ := l.AddRadioChannels(ctx(), r.ID, []string{ids[0], ids[3]}); n != 1 || order() != "CABD" {
		t.Fatalf("append skips existing: %d, %s", n, order())
	}
	if err := l.MoveRadioChannel(ctx(), r.ID, ids[3], 0); err != nil || order() != "DCAB" {
		t.Fatalf("move to top: %v, %s", err, order())
	}
	if err := l.MoveRadioChannel(ctx(), r.ID, ids[3], 99); err != nil || order() != "CABD" {
		t.Fatalf("move past end: %v, %s", err, order())
	}
	fifty, skip := 50, true
	if err := l.SetRadioEntry(ctx(), r.ID, domain.RadioEntry{Channel: domain.Channel{ID: ids[1]}, SectionStart: &fifty, Name: " Bee ", Power: domain.PowerLow, Skip: &skip}); err != nil {
		t.Fatal(err)
	}
	es, _ := l.RadioEntries(ctx(), r.ID)
	if b := es[2]; b.Channel.Name != "B" || b.SectionStart == nil || *b.SectionStart != 50 || b.Name != "Bee" || b.Power != domain.PowerLow || b.Skip == nil || !*b.Skip {
		t.Fatalf("overrides = %+v", b)
	}
	if es[0].Skip != nil || es[0].SectionStart != nil {
		t.Fatalf("unset overrides should stay nil: %+v", es[0])
	}
	if on, _ := l.RadioChannelIDs(ctx(), r.ID); len(on) != 4 || !on[ids[3]] {
		t.Errorf("RadioChannelIDs = %v", on)
	}
	// Deleting a channel takes it off the radio.
	if err := l.DeleteChannel(ctx(), ids[0]); err != nil {
		t.Fatal(err)
	}
	if order() != "CBD" {
		t.Fatalf("after channel delete: %s", order())
	}
	if n, _ := l.RemoveRadioChannels(ctx(), r.ID, []string{ids[2], "missing"}); n != 1 || order() != "BD" {
		t.Fatalf("remove: %d, %s", n, order())
	}
	mustNotFound(t, "moving a channel not on the radio", l.MoveRadioChannel(ctx(), r.ID, ids[0], 0))
	if err := l.DeleteRadio(ctx(), r.ID); err != nil {
		t.Fatal(err)
	}
	if on, _ := l.RadioChannelIDs(ctx(), ""); len(on) != 0 {
		t.Errorf("deleted radio still has channels: %v", on)
	}
	if _, err := l.GetChannel(ctx(), ids[1]); err != nil {
		t.Errorf("deleting a radio deleted its channels: %v", err)
	}
}

func testSourcePlacement(t *testing.T, s store.Store) {
	l := newLibrary(t, s, "L")
	ids := map[string]string{}
	for i, name := range []string{"A", "B", "C", "D", "E", "H"} {
		ids[name] = channel(t, l, name, int64(146_520_000+i*15_000)).ID
	}
	r, err := l.CreateRadio(ctx(), domain.Radio{Name: "HT", Model: "baofeng-uv5rm"})
	if err != nil {
		t.Fatal(err)
	}
	hundred := 100
	first, err := l.AddRadioSource(ctx(), domain.RadioSource{RadioID: r.ID, Query: "tag:a", Sort: "name"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := l.AddRadioSource(ctx(), domain.RadioSource{RadioID: r.ID, Query: "tag:b", Sort: "freq", SectionStart: &hundred})
	if err != nil || second.Position != 1 {
		t.Fatalf("second source: %+v, %v", second, err)
	}
	if _, err := l.AddRadioSource(ctx(), domain.RadioSource{RadioID: r.ID, Query: "x", Sort: "size"}); err == nil {
		t.Error("bad sort accepted")
	}
	order := func() string { return radioOrder(t, l, r.ID) }
	// The second source syncs first, then the first: it still goes ahead.
	if _, err := l.AddSourceChannels(ctx(), second, []string{ids["C"], ids["D"]}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.AddRadioChannels(ctx(), r.ID, []string{ids["H"]}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.AddSourceChannels(ctx(), first, []string{ids["A"]}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.AddSourceChannels(ctx(), first, []string{ids["B"], ids["A"]}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.AddSourceChannels(ctx(), second, []string{ids["E"]}); err != nil {
		t.Fatal(err)
	}
	if got := order(); got != "ABCDEH" {
		t.Fatalf("order = %s, want first source, second source, then the hand-added channel", got)
	}
	es, _ := l.RadioEntries(ctx(), r.ID)
	if es[2].SectionStart == nil || *es[2].SectionStart != 100 || es[2].SourceID != second.ID || es[5].SourceID != "" {
		t.Fatalf("entries = %+v", es)
	}
	// A new section start moves now; clearing it takes it off.
	fifty := 50
	second.SectionStart = &fifty
	if err := l.UpdateRadioSource(ctx(), second); err != nil {
		t.Fatal(err)
	}
	if es, _ := l.RadioEntries(ctx(), r.ID); es[2].SectionStart == nil || *es[2].SectionStart != 50 {
		t.Fatalf("moved section start = %+v", es[2])
	}
	second.SectionStart = nil
	if err := l.UpdateRadioSource(ctx(), second); err != nil {
		t.Fatal(err)
	}
	if es, _ := l.RadioEntries(ctx(), r.ID); es[2].SectionStart != nil {
		t.Fatalf("cleared section start = %+v", es[2])
	}
	if err := l.MoveRadioSource(ctx(), second, 0); err != nil {
		t.Fatal(err)
	}
	srcs, _ := l.RadioSources(ctx(), r.ID)
	if srcs[0].ID != second.ID || srcs[1].Position != 1 {
		t.Fatalf("sources after move = %+v", srcs)
	}
	if all, _ := l.AllRadioSources(ctx()); len(all) != 2 {
		t.Errorf("AllRadioSources = %+v", all)
	}
	if n, err := l.RemoveRadioSource(ctx(), second, true); err != nil || n != 3 || order() != "ABH" {
		t.Fatalf("remove with channels: %d, %v, %s", n, err, order())
	}
	if n, err := l.RemoveRadioSource(ctx(), srcs[1], false); err != nil || n != 0 || order() != "ABH" {
		t.Fatalf("remove keeping channels: %d, %v, %s", n, err, order())
	}
	if es, _ := l.RadioEntries(ctx(), r.ID); es[0].SourceID != "" {
		t.Errorf("removed source still on its channels: %+v", es[0])
	}
}

func testSavedSearches(t *testing.T, s store.Store) {
	l := newLibrary(t, s, "L")
	x, err := l.CreateSearch(ctx(), domain.SavedSearch{Name: " Local ", Query: "tag:local"})
	if err != nil {
		t.Fatal(err)
	}
	if x.Name != "local" {
		t.Fatalf("name = %q, want lower-cased", x.Name)
	}
	for _, ref := range []string{"local", "@local", "LOCAL", x.ID} {
		if got, err := l.GetSearch(ctx(), ref); err != nil || got.ID != x.ID {
			t.Fatalf("GetSearch(%q) = %v, %v", ref, got, err)
		}
	}
	x.Query = "tag:home"
	if _, err := l.UpdateSearch(ctx(), x); err != nil {
		t.Fatal(err)
	}
	if xs, _ := l.ListSearches(ctx()); len(xs) != 1 || xs[0].Query != "tag:home" {
		t.Fatalf("searches = %+v", xs)
	}
	if err := l.DeleteSearch(ctx(), x.ID); err != nil {
		t.Fatal(err)
	}
	_, err = l.GetSearch(ctx(), "local")
	mustNotFound(t, "deleted search", err)
}

func testLibraryIsolation(t *testing.T, s store.Store) {
	a, b := newLibrary(t, s, "A"), newLibrary(t, s, "B")
	g, err := a.CreateGroup(ctx(), domain.Group{Name: "Home"})
	if err != nil {
		t.Fatal(err)
	}
	c := channel(t, a, "Calling", 146_520_000)
	ra, err := a.CreateRadio(ctx(), domain.Radio{Name: "HT", Model: "baofeng-uv5rm"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddRadioChannels(ctx(), ra.ID, []string{c.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.CreateSearch(ctx(), domain.SavedSearch{Name: "wx", Query: "band:noaa"}); err != nil {
		t.Fatal(err)
	}

	if cs, _ := b.ListChannels(ctx()); len(cs) != 0 {
		t.Errorf("B lists A's channels: %+v", cs)
	}
	if gs, _ := b.ListGroups(ctx()); len(gs) != 0 {
		t.Errorf("B lists A's groups: %+v", gs)
	}
	if rs, _ := b.ListRadios(ctx()); len(rs) != 0 {
		t.Errorf("B lists A's radios: %+v", rs)
	}
	if xs, _ := b.ListSearches(ctx()); len(xs) != 0 {
		t.Errorf("B lists A's searches: %+v", xs)
	}
	if srcs, _ := b.AllRadioSources(ctx()); len(srcs) != 0 {
		t.Errorf("B lists A's sources: %+v", srcs)
	}
	if on, _ := b.RadioChannelIDs(ctx(), ""); len(on) != 0 {
		t.Errorf("B sees A's radio channels: %v", on)
	}
	_, err = b.GetChannel(ctx(), c.ID)
	mustNotFound(t, "B gets A's channel", err)
	_, err = b.GetGroup(ctx(), "Home")
	mustNotFound(t, "B gets A's group", err)
	_, err = b.GetRadio(ctx(), ra.ID)
	mustNotFound(t, "B gets A's radio", err)
	_, err = b.GetSearch(ctx(), "wx")
	mustNotFound(t, "B gets A's search", err)
	_, err = b.RadioEntries(ctx(), ra.ID)
	mustNotFound(t, "B lists A's radio entries", err)
	mustNotFound(t, "B deletes A's channel", b.DeleteChannel(ctx(), c.ID))
	mustNotFound(t, "B deletes A's radio", b.DeleteRadio(ctx(), ra.ID))
	mustNotFound(t, "B deletes A's group", b.DeleteGroup(ctx(), g.ID))
	moved := c
	moved.Name = "Hijacked"
	_, err = b.UpdateChannel(ctx(), moved)
	mustNotFound(t, "B updates A's channel", err)

	// References can't cross libraries.
	_, err = b.CreateChannel(ctx(), domain.Channel{Name: "X", GroupID: &g.ID, ReceiveHz: 146_520_000, Mode: "FM", ReceiveTone: none, TransmitTone: none})
	mustNotFound(t, "B channel in A's group", err)
	rb, err := b.CreateRadio(ctx(), domain.Radio{Name: "HT", Model: "baofeng-uv5rm"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = b.AddRadioChannels(ctx(), rb.ID, []string{c.ID})
	mustNotFound(t, "B radio gets A's channel", err)
	_, err = b.AddRadioChannels(ctx(), ra.ID, []string{c.ID})
	mustNotFound(t, "B adds to A's radio", err)
	_, err = b.AddRadioSource(ctx(), domain.RadioSource{RadioID: ra.ID, Query: "x", Sort: "name"})
	mustNotFound(t, "B adds a source to A's radio", err)

	if got, err := a.GetChannel(ctx(), c.ID); err != nil || got.Name != "Calling" {
		t.Errorf("A's channel changed: %+v, %v", got, err)
	}
}

func testDeleteLibraryCascades(t *testing.T, s store.Store) {
	a, b := newLibrary(t, s, "A"), newLibrary(t, s, "B")
	for _, l := range []store.Library{a, b} {
		c := channel(t, l, "Calling", 146_520_000)
		r, err := l.CreateRadio(ctx(), domain.Radio{Name: "HT", Model: "baofeng-uv5rm"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := l.AddRadioChannels(ctx(), r.ID, []string{c.ID}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.DeleteLibrary(ctx(), a.ID()); err != nil {
		t.Fatal(err)
	}
	if cs, _ := a.ListChannels(ctx()); len(cs) != 0 {
		t.Errorf("deleted library still has channels: %+v", cs)
	}
	if cs, _ := b.ListChannels(ctx()); len(cs) != 1 {
		t.Errorf("other library lost channels: %+v", cs)
	}
	if on, _ := b.RadioChannelIDs(ctx(), ""); len(on) != 1 {
		t.Errorf("other library's radio lost channels: %v", on)
	}
}

// A backup restored into a second library keeps IDs where it can and gets
// new ones where they're taken.
func testSuppliedIDs(t *testing.T, s store.Store) {
	a, b := newLibrary(t, s, "A"), newLibrary(t, s, "B")
	const gid, cid = "11111111-2222-3333-4444-555555555555", "66666666-7777-8888-9999-000000000000"
	ga, err := a.CreateGroup(ctx(), domain.Group{ID: gid, Name: "Home"})
	if err != nil || ga.ID != gid {
		t.Fatalf("group ID not kept: %+v, %v", ga, err)
	}
	ca, err := a.CreateChannel(ctx(), domain.Channel{ID: cid, Name: "Calling", GroupID: &ga.ID, ReceiveHz: 146_520_000, Mode: "FM", ReceiveTone: none, TransmitTone: none})
	if err != nil || ca.ID != cid {
		t.Fatalf("channel ID not kept: %+v, %v", ca, err)
	}
	gb, err := b.CreateGroup(ctx(), domain.Group{ID: gid, Name: "Home"})
	if err != nil || gb.ID == gid || gb.ID == "" {
		t.Fatalf("taken group ID: %+v, %v", gb, err)
	}
	cb, err := b.CreateChannel(ctx(), domain.Channel{ID: cid, Name: "Calling", GroupID: &gb.ID, ReceiveHz: 146_520_000, Mode: "FM", ReceiveTone: none, TransmitTone: none})
	if err != nil || cb.ID == cid || cb.ID == "" {
		t.Fatalf("taken channel ID: %+v, %v", cb, err)
	}
	if got, _ := a.GetChannel(ctx(), cid); got.Group == nil || got.Group.ID != gid {
		t.Errorf("A's channel changed: %+v", got)
	}
}

func testTransactionRollsBack(t *testing.T, s store.Store) {
	l := newLibrary(t, s, "L")
	boom := errors.New("boom")
	err := l.InTx(ctx(), func(tx store.Library) error {
		if _, err := tx.CreateGroup(ctx(), domain.Group{Name: "Temp"}); err != nil {
			return err
		}
		channel(t, tx, "Temp", 146_520_000)
		if cs, _ := tx.ListChannels(ctx()); len(cs) != 1 {
			t.Errorf("the transaction doesn't see its own write: %+v", cs)
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("InTx = %v", err)
	}
	if cs, _ := l.ListChannels(ctx()); len(cs) != 0 {
		t.Errorf("rolled-back channel kept: %+v", cs)
	}
	if gs, _ := l.ListGroups(ctx()); len(gs) != 0 {
		t.Errorf("rolled-back group kept: %+v", gs)
	}
}
