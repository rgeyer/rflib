package seed

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/ryan/rflib/internal/domain"
	"github.com/ryan/rflib/internal/query"
)

func TestEveryListIsDescribed(t *testing.T) {
	entries, err := fs.ReadDir(files, "data")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != len(lists) {
		t.Fatalf("%d data files but %d described lists", len(entries), len(lists))
	}
	for _, e := range entries {
		if _, err := Get(strings.TrimSuffix(e.Name(), ".csv")); err != nil {
			t.Error(err)
		}
	}
}

// Every built-in channel must parse and fit its own service's band plan.
func TestListsAreValidAndFitTheirBandPlans(t *testing.T) {
	want := map[string]int{"us-cb": 40, "us-frs": 22, "us-gmrs": 30, "us-ham-calling": 8, "us-murs": 5, "us-noaa": 7,
		"us-60m": 5, "us-aprs": 2, "us-ft8": 11, "us-interop": 15, "us-iss": 2, "us-time": 9,
		"us-beacons": 5, "us-fm-sats": 5, "us-ft4": 10, "us-js8": 9, "us-psk31": 8, "us-radiofax": 20, "us-sstv": 5, "us-wspr": 13}
	for _, l := range Lists() {
		b, err := l.Batch()
		if err != nil {
			t.Fatal(err)
		}
		if len(b.Records) != want[l.ID] {
			t.Errorf("%s has %d channels, want %d", l.ID, len(b.Records), want[l.ID])
		}
		for _, r := range b.Records {
			if r.Channel.Service == domain.ServiceUnspecified {
				t.Errorf("%s: %s has no service", l.ID, r.Channel.Name)
			}
			if w := domain.CheckBandPlan(r.Channel); len(w) > 0 {
				t.Errorf("%s: %s: %v", l.ID, r.Channel.Name, w)
			}
		}
	}
}

// No channel is in two lists, so installing several never puts the same
// channel on the shared list twice. FRS and GMRS share their channels by
// rule, so that pair is allowed.
func TestListsDontOverlap(t *testing.T) {
	seen := map[string]string{}
	for _, l := range Lists() {
		b, err := l.Batch()
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range b.Records {
			key := domain.ProgrammedKey(r.Channel)
			other, ok := seen[key]
			switch {
			case !ok:
				seen[key] = l.ID
			case other == "us-frs" && l.ID == "us-gmrs":
			default:
				t.Errorf("%s: %s is also in %s", l.ID, r.Channel.Name, other)
			}
		}
	}
}

// Lists carry only shared tags, which never repeat a channel's fields, so
// nothing is dropped on install and the shared list has no duplication.
func TestListTagsAreSharedTags(t *testing.T) {
	for _, l := range Lists() {
		b, err := l.Batch()
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range b.Records {
			if err := domain.ValidateSharedTags(r.Channel.Tags); err != nil {
				t.Errorf("%s: %s: %v", l.ID, r.Channel.Name, err)
			}
			if w := query.TagRepeats(r.Channel); len(w) > 0 {
				t.Errorf("%s: %s: %v", l.ID, r.Channel.Name, w)
			}
		}
	}
}
