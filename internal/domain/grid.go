package domain

import (
	"math"
	"strings"
)

// Grid is a Maidenhead grid square, such as CM94 or CM94sq: its center,
// and the distance from the center to its corners.
type Grid struct {
	Latitude, Longitude float64
	// RadiusKM reaches the square's corners from its center, so a location
	// at the center with this radius covers the whole square.
	RadiusKM float64
}

// ParseGrid reads a Maidenhead grid square of 4, 6, or 8 characters, in
// any letter case: a field (two letters A-R), a square (two digits), a
// subsquare (two letters A-X), and an extended square (two digits).
func ParseGrid(s string) (Grid, error) {
	g := strings.ToUpper(strings.TrimSpace(s))
	bad := Invalidf("%q is not a grid square; use 4, 6, or 8 characters, such as CM94 or CM94sq", s)
	if n := len(g); n != 4 && n != 6 && n != 8 {
		return Grid{}, bad
	}
	// Each pair is longitude then latitude, in steps that divide the last.
	lon, lat := -180.0, -90.0
	lonStep, latStep := 360.0, 180.0
	for i := 0; i < len(g); i += 2 {
		var base, count byte
		switch i {
		case 0:
			base, count = 'A', 18
		case 4:
			base, count = 'A', 24
		default:
			base, count = '0', 10
		}
		x, y := g[i]-base, g[i+1]-base
		if g[i] < base || g[i+1] < base || x >= count || y >= count {
			return Grid{}, bad
		}
		lonStep, latStep = lonStep/float64(count), latStep/float64(count)
		lon += float64(x) * lonStep
		lat += float64(y) * latStep
	}
	round := func(v float64) float64 { return math.Round(v*1e6) / 1e6 }
	c := Grid{Latitude: round(lat + latStep/2), Longitude: round(lon + lonStep/2)}
	c.RadiusKM = DistanceKM(c.Latitude, c.Longitude, lat, lon)
	// Round to what the square's size can tell apart.
	c.RadiusKM = math.Ceil(c.RadiusKM*10) / 10
	return c, nil
}

// IsGrid reports whether s looks like a grid square rather than a number.
func IsGrid(s string) bool {
	s = strings.TrimSpace(s)
	return len(s) >= 2 && (s[0]|0x20) >= 'a' && (s[0]|0x20) <= 'z'
}

// Location is the square as a channel location covering it.
func (g Grid) Location() *Location {
	r := g.RadiusKM
	return &Location{Latitude: g.Latitude, Longitude: g.Longitude, RadiusKM: &r}
}
