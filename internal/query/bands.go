package query

import (
	"fmt"
	"slices"
	"strings"

	"github.com/ryan/rflib/internal/domain"
)

// band is a named range of receive frequencies, or a channel plan.
type band struct {
	name          string
	lowHz, highHz int64
	channels      []int64
}

// Bands are the names band: takes: US amateur bands by wavelength, and
// services with fixed channel plans. A band matches on the receive
// frequency.
var Bands = []band{
	{name: "160m", lowHz: 1_800_000, highHz: 2_000_000},
	{name: "80m", lowHz: 3_500_000, highHz: 4_000_000},
	{name: "60m", lowHz: 5_330_500, highHz: 5_406_500},
	{name: "40m", lowHz: 7_000_000, highHz: 7_300_000},
	{name: "30m", lowHz: 10_100_000, highHz: 10_150_000},
	{name: "20m", lowHz: 14_000_000, highHz: 14_350_000},
	{name: "17m", lowHz: 18_068_000, highHz: 18_168_000},
	{name: "15m", lowHz: 21_000_000, highHz: 21_450_000},
	{name: "12m", lowHz: 24_890_000, highHz: 24_990_000},
	{name: "10m", lowHz: 28_000_000, highHz: 29_700_000},
	{name: "6m", lowHz: 50_000_000, highHz: 54_000_000},
	{name: "2m", lowHz: 144_000_000, highHz: 148_000_000},
	{name: "1.25m", lowHz: 222_000_000, highHz: 225_000_000},
	{name: "70cm", lowHz: 420_000_000, highHz: 450_000_000},
	{name: "33cm", lowHz: 902_000_000, highHz: 928_000_000},
	{name: "23cm", lowHz: 1_240_000_000, highHz: 1_300_000_000},
	{name: "air", lowHz: 108_000_000, highHz: 137_000_000},
	{name: "marine", lowHz: 156_000_000, highHz: 162_025_000},
	{name: "cb", channels: domain.CBChannelsHz},
	{name: "frs", channels: domain.FRSChannelsHz},
	{name: "gmrs", channels: append(slices.Clone(domain.FRSChannelsHz), domain.GMRSRepeaterInputsHz...)},
	{name: "murs", channels: domain.MURSChannelsHz},
	{name: "noaa", channels: domain.NOAAChannelsHz},
}

var bandAliases = map[string]string{"220": "1.25m", "440": "70cm", "weather": "noaa", "wx": "noaa"}

// BandNames lists the names band: takes.
func BandNames() []string {
	names := make([]string, len(Bands))
	for i, b := range Bands {
		names[i] = b.name
	}
	return names
}

func bandMatcher(v string) (Matcher, error) {
	name := strings.ToLower(v)
	if alias, ok := bandAliases[name]; ok {
		name = alias
	}
	i := slices.IndexFunc(Bands, func(b band) bool { return b.name == name })
	if i < 0 {
		return nil, fmt.Errorf("unknown band %q; use %s", v, strings.Join(BandNames(), ", "))
	}
	b := Bands[i]
	if b.channels != nil {
		return func(ch domain.Channel) bool { return slices.Contains(b.channels, ch.ReceiveHz) }, nil
	}
	return func(ch domain.Channel) bool { return ch.ReceiveHz >= b.lowHz && ch.ReceiveHz <= b.highHz }, nil
}
