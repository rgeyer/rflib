package app

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/ryan/rflib/internal/domain"
	"github.com/ryan/rflib/internal/importer"
	"github.com/ryan/rflib/internal/seed"
)

// rows makes a built-in list's rows: name, MHz, and notes for each.
func rows(spec ...string) []importer.Record {
	var out []importer.Record
	for i := 0; i < len(spec); i += 3 {
		hz, _ := domain.ParseFrequencyMHz(spec[i+1])
		out = append(out, importer.Record{Channel: domain.Channel{Name: spec[i], ReceiveHz: hz, Mode: "FM", Service: domain.ServiceAmateur,
			ReceiveTone: none, TransmitTone: none, Notes: spec[i+2]}})
	}
	return out
}

func changes(st SeedStatus) string {
	var out []string
	for _, ch := range st.Changes {
		s := ch.Kind + " " + ch.Name
		if len(ch.Fields) > 0 {
			s += ": " + strings.Join(ch.Fields, ",")
		}
		if len(ch.Kept) > 0 {
			s += "; kept " + strings.Join(ch.Kept, ",")
		}
		out = append(out, s)
	}
	return strings.Join(out, " | ")
}

// An update adds new rows, applies what the list changed, keeps a
// maintainer's corrections, and retires rows the list dropped.
func TestSeedUpdates(t *testing.T) {
	ctx := context.Background()
	_, d := newService(t)
	sh := openShared(t, d)
	l := seed.List{ID: "test-list"}
	v1 := rows("Alpha", "146.52", "first", "Bravo", "146.55", "second", "Charlie", "146.58", "third")
	st, err := sh.updateSeed(ctx, l, v1, true)
	if err != nil || st.Installed || changes(st) != "add Alpha | add Bravo | add Charlie" {
		t.Fatalf("dry run = %q (installed %v), %v", changes(st), st.Installed, err)
	}
	if all, _ := sh.lib.ListChannels(ctx); len(all) != 0 {
		t.Fatal("the dry run wrote")
	}
	if _, err := sh.updateSeed(ctx, l, v1, false); err != nil {
		t.Fatal(err)
	}
	if st, _ := sh.updateSeed(ctx, l, v1, true); st.Pending() || !st.Installed {
		t.Fatalf("installed list = %q", changes(st))
	}

	// A maintainer corrects Alpha's notes and Bravo's frequency.
	byName := map[string]domain.Channel{}
	all, _ := sh.lib.ListChannels(ctx)
	for _, c := range all {
		byName[c.Name] = c
	}
	a, b := byName["Alpha"], byName["Bravo"]
	a.Notes = "corrected"
	b.ReceiveHz = 146_565_000
	for _, c := range []domain.Channel{a, b} {
		if _, err := sh.lib.UpdateChannel(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	if st, _ := sh.updateSeed(ctx, l, v1, true); st.Pending() {
		t.Fatalf("corrections alone are pending: %q", changes(st))
	}

	// The next release changes Alpha's notes and Bravo's notes, drops
	// Charlie, and adds Delta.
	v2 := rows("Alpha", "146.52", "first, revised", "Bravo", "146.55", "second, revised", "Delta", "146.61", "fourth")
	st, err = sh.updateSeed(ctx, l, v2, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := changes(st); got != "update Alpha; kept notes | update Bravo: notes | add Delta | drop Charlie" {
		t.Fatalf("changes = %q", got)
	}
	all, _ = sh.lib.ListChannels(ctx)
	for _, c := range all {
		byName[c.Name] = c
	}
	if c := byName["Alpha"]; c.Notes != "corrected" {
		t.Errorf("Alpha's correction was overwritten: %q", c.Notes)
	}
	if c := byName["Bravo"]; c.Notes != "second, revised" || c.ReceiveHz != 146_565_000 {
		t.Errorf("Bravo = %q at %d; want the list's notes and the corrected frequency", c.Notes, c.ReceiveHz)
	}
	if c := byName["Charlie"]; c.Status != domain.StatusRetired || !strings.Contains(c.StatusNote, "test-list") {
		t.Errorf("Charlie = %s %q", c.Status, c.StatusNote)
	}
	if _, ok := byName["Delta"]; !ok {
		t.Error("Delta wasn't added")
	}
	if st, _ := sh.updateSeed(ctx, l, v2, true); st.Pending() {
		t.Errorf("after the update = %q", changes(st))
	}
	revs, _ := sh.lib.ChannelRevisions(ctx, byName["Bravo"].ID)
	if last := revs[len(revs)-1]; last.Reason != "built-in list test-list" {
		t.Errorf("Bravo's revision reason = %q", last.Reason)
	}
}

// Seeding records each channel's list, RecordSeeds changes nothing on a
// list already recorded, and a list never seeded isn't installed.
func TestRecordSeeds(t *testing.T) {
	ctx := context.Background()
	_, d := newService(t)
	sh := openShared(t, d)
	noaa, _ := seed.Get("us-noaa")
	if _, err := sh.UpdateSeed(ctx, noaa, false); err != nil {
		t.Fatal(err)
	}
	recs, _ := sh.lib.SeedChannels(ctx)
	if len(recs) != 7 || !slices.ContainsFunc(recs, func(r domain.SeedChannel) bool { return r.Name == "NOAA WX1" }) {
		t.Fatalf("records = %d", len(recs))
	}
	if err := sh.RecordSeeds(ctx); err != nil {
		t.Fatal(err)
	}
	if st, _ := sh.SeedStatus(ctx, noaa); st.Pending() || !st.Installed {
		t.Errorf("noaa = %q", changes(st))
	}
	cb, _ := seed.Get("us-cb")
	if st, _ := sh.SeedStatus(ctx, cb); st.Installed || len(st.Changes) != 80 {
		t.Error("a list never seeded counts as installed")
	}
}

// No annotation row's own ID ever leaves the service: every channel it
// returns is the library's own, or a shared channel by its shared ID.
func TestNoAnnotationIDsLeak(t *testing.T) {
	ctx := context.Background()
	s, d := newService(t)
	sh := openShared(t, d)
	for _, id := range []string{"us-gmrs", "us-noaa"} {
		l, _ := seed.Get(id)
		if _, err := sh.UpdateSeed(ctx, l, false); err != nil {
			t.Fatal(err)
		}
	}
	shared, _ := sh.lib.ListChannels(ctx)
	sharedIDs := map[string]bool{}
	for _, c := range shared {
		sharedIDs[c.ID] = true
	}
	// Annotate some, put some on a radio, convert one, copy one.
	noaa, err := s.FindForRadios(ctx, mustParse(t, "service:noaa"))
	if err != nil || len(noaa) != 7 {
		t.Fatalf("noaa = %d, %v", len(noaa), err)
	}
	if _, _, err := s.UpdateChannels(ctx, Selection{IDs: []string{noaa[0].ID, noaa[1].ID}}, ChannelEdit{Tags: &TagEdit{Add: []string{"wx"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateRadio(ctx, domain.Radio{Name: "HT", Model: "baofeng-uv5rm"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AddChannels(ctx, "HT", AddRequest{Query: mustParse(t, "service:noaa OR band:gmrs is:repeater")}); err != nil {
		t.Fatal(err)
	}
	mine := add(t, s, "Family", 462.675, domain.ServiceGMRS)
	hz := mine.ReceiveHz
	mine.TransmitHz, mine.Tags = &hz, []string{"family"}
	if _, _, err := s.UpdateChannel(ctx, mine); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ConvertToLinks(ctx, nil, nil, false); err != nil {
		t.Fatal(err)
	}
	copies, err := s.CopyChannels(ctx, Selection{IDs: []string{noaa[1].ID}})
	if err != nil || len(copies) != 1 {
		t.Fatalf("copy = %+v, %v", copies, err)
	}
	private := map[string]bool{}
	all, _ := s.lib.ListChannels(ctx)
	for _, c := range all {
		if c.Link == nil {
			private[c.ID] = true
		}
	}

	check := func(what string, cs ...domain.Channel) {
		t.Helper()
		for _, c := range cs {
			switch {
			case c.Link != nil && (c.ID != c.Link.SharedID || !sharedIDs[c.ID]):
				t.Errorf("%s: %s has ID %s for shared %s", what, c.Name, c.ID, c.Link.SharedID)
			case c.Link == nil && !private[c.ID] && !sharedIDs[c.ID]:
				t.Errorf("%s: %s has an unknown ID %s", what, c.Name, c.ID)
			}
		}
	}
	check("library", all...)
	for _, q := range []string{"", "in:shared", "tag:wx", "service:gmrs"} {
		found, err := s.Find(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		check("find "+q, found...)
		forRadios, err := s.FindForRadios(ctx, mustParse(t, q))
		if err != nil {
			t.Fatal(err)
		}
		check("for radios "+q, forRadios...)
	}
	_, es, err := s.RadioEntries(ctx, "HT")
	if err != nil || len(es) == 0 {
		t.Fatalf("entries = %d, %v", len(es), err)
	}
	for _, e := range es {
		check("radio", e.Channel)
	}
	layout, _, err := s.Layout(ctx, "HT")
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range layout.Memories {
		check("layout", m.Channel)
	}
	ids := make([]string, len(es))
	for i, e := range es {
		ids[i] = e.Channel.ID
	}
	selected, err := s.Select(ctx, Selection{IDs: ids})
	if err != nil {
		t.Fatal(err)
	}
	check("select", selected...)
	for _, id := range ids {
		c, err := s.GetChannel(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		check("get", c)
	}
	if _, err := sh.SetStatus(ctx, noaa[0].ID, StatusChange{Status: domain.StatusOffAir}, "test"); err != nil {
		t.Fatal(err)
	}
	changes, err := s.Changes(ctx)
	if err != nil || len(changes) == 0 {
		t.Fatalf("changes = %d, %v", len(changes), err)
	}
	for _, ch := range changes {
		check("changes", ch.Channel)
	}
}

// The in: scopes: use (the default) is the library's own channels and the
// shared ones it annotates or has on a radio; mine, shared, and all as named.
// Radio searches default to all, without shared channels the library copied.
func TestScopes(t *testing.T) {
	ctx := context.Background()
	s, d := newService(t)
	sh := openShared(t, d)
	tagged := sharedChannel(t, sh, "Tagged", 146.52)
	onRadio := sharedChannel(t, sh, "On Radio", 146.55)
	unused := sharedChannel(t, sh, "Unused", 146.58)
	copiedFrom := sharedChannel(t, sh, "Copied", 146.61)
	add(t, s, "Mine", 147.0, domain.ServiceAmateur)
	if _, _, err := s.UpdateChannels(ctx, Selection{IDs: []string{tagged.ID}}, ChannelEdit{Tags: &TagEdit{Add: []string{"home"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateRadio(ctx, domain.Radio{Name: "HT", Model: "baofeng-uv5rm"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AddChannels(ctx, "HT", AddRequest{IDs: []string{onRadio.ID}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CopyChannels(ctx, Selection{IDs: []string{copiedFrom.ID}}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ q, want string }{
		{"", "Copied, Mine, On Radio, Tagged"},
		{"in:use", "Copied, Mine, On Radio, Tagged"},
		{"in:mine", "Copied, Mine"},
		{"in:shared", "Copied, On Radio, Tagged, Unused"},
		{"in:all", "Copied, Copied, Mine, On Radio, Tagged, Unused"},
	} {
		cs, err := s.Find(ctx, tc.q)
		if got := strings.ReplaceAll(names(cs), ",", ", "); err != nil || got != tc.want {
			t.Errorf("%q = %s, %v; want %s", tc.q, got, err, tc.want)
		}
	}
	forRadios, err := s.FindForRadios(ctx, mustParse(t, ""))
	if got := strings.ReplaceAll(names(forRadios), ",", ", "); err != nil || got != "Copied, Mine, On Radio, Tagged, Unused" {
		t.Errorf("for radios = %s, %v", got, err)
	}
	for _, c := range forRadios {
		if c.ID == copiedFrom.ID {
			t.Error("a copied shared channel is offered to radios")
		}
	}
	_ = unused
	// What the shared list adds to a search: unused shared channels, not
	// the originals of copies.
	for _, tc := range []struct {
		q    string
		want int
	}{{"", 1}, {"in:mine", 3}, {"in:shared", 0}, {"in:all", 0}, {"name:unused", 1}, {"name:copied", 0}} {
		cs, err := s.Find(ctx, tc.q)
		if err != nil {
			t.Fatal(err)
		}
		if more, err := s.MoreInShared(ctx, mustParse(t, tc.q), len(cs)); err != nil || more != tc.want {
			t.Errorf("more in shared for %q = %d, %v; want %d", tc.q, more, err, tc.want)
		}
	}
}
