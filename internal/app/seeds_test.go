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
	if st, _ := sh.SeedStatus(ctx, cb); st.Installed || len(st.Changes) != 40 {
		t.Error("a list never seeded counts as installed")
	}
}
