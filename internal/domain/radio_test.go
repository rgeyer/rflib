package domain

import (
	"slices"
	"strings"
	"testing"
)

func TestCompareNames(t *testing.T) {
	names := []string{"GMRS 15R", "GMRS 10", "GMRS 2", "GMRS 15", "GMRS 1", "CB 19", "CB 9", "WX10", "WX2", "GMRS 02", "GMRS", "gmrs 1"}
	slices.SortStableFunc(names, CompareNames)
	want := "CB 9,CB 19,GMRS,GMRS 1,GMRS 2,GMRS 02,GMRS 10,GMRS 15,GMRS 15R,WX2,WX10,gmrs 1"
	if got := strings.Join(names, ","); got != want {
		t.Errorf("sorted = %s\nwant     %s", got, want)
	}
	for _, tc := range []struct {
		a, b string
		want int
	}{{"a", "a", 0}, {"a1", "a01", -1}, {"9", "10", -1}, {"1a", "a", -1}, {"x 007", "x 7b", -1}} {
		if got := CompareNames(tc.a, tc.b); got != tc.want {
			t.Errorf("CompareNames(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}
