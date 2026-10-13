package seed

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/ryan/rflib/internal/domain"
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
	want := map[string]int{"us-cb": 40, "us-frs": 22, "us-gmrs": 30, "us-ham-calling": 8, "us-murs": 5, "us-noaa": 7}
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
