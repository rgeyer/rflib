package domain

import (
	"math"
	"testing"
)

func TestParseGrid(t *testing.T) {
	for _, tc := range []struct {
		in       string
		lat, lon float64
		radius   float64 // approximate, km
	}{
		{"CM94", 34.5, -121, 105},
		{"cm94sq", 34.6875, -120.4583, 4.4},
		{"CM94SQ", 34.6875, -120.4583, 4.4},
		{"FN31pr", 41.7292, -72.7083, 3.9},
		{"CM94sq12", 34.6771, -120.4875, 0.5},
		{"JJ00", 0.5, 1, 124},
	} {
		g, err := ParseGrid(tc.in)
		if err != nil {
			t.Errorf("%s: %v", tc.in, err)
			continue
		}
		if math.Abs(g.Latitude-tc.lat) > 0.001 || math.Abs(g.Longitude-tc.lon) > 0.001 {
			t.Errorf("%s = %.4f,%.4f, want %.4f,%.4f", tc.in, g.Latitude, g.Longitude, tc.lat, tc.lon)
		}
		if math.Abs(g.RadiusKM-tc.radius) > tc.radius*0.1 {
			t.Errorf("%s radius = %g, want about %g", tc.in, g.RadiusKM, tc.radius)
		}
	}
	for _, bad := range []string{"", "CM9", "CM94s", "SM94", "CM9A", "CM94sz", "CM94sqAB", "12AB", "CM94sq123"} {
		if _, err := ParseGrid(bad); err == nil {
			t.Errorf("%q parsed", bad)
		}
	}
}
