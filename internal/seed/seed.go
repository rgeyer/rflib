// Package seed provides built-in reference channel lists. Each list is a
// generic-format CSV embedded in the binary and installed through the
// importer, so installing one twice changes nothing.
package seed

import (
	"embed"
	"fmt"
	"sort"
	"strings"

	"github.com/ryan/rflib/internal/importer"
)

//go:embed data/*.csv
var files embed.FS

type List struct {
	ID          string
	Description string
	// Renamed maps a channel's name to the names it had in earlier
	// versions of the list, so an update renames the shared channel rather
	// than retiring it and adding another.
	Renamed map[string][]string
}

// lists describes every file in data/; TestEveryListIsDescribed keeps them in
// step.
//
// The lists are hand-entered. Each one's source, and how it's checked, is in
// tools/datacheck/sources.yaml.
var lists = []List{
	{ID: "us-60m", Description: "US amateur 60m channels 1-5 (USB dial frequencies)"},
	{ID: "us-aprs", Description: "APRS: the national frequency and the ISS digipeater (ARISS)", Renamed: map[string][]string{"ARISS APRS": {"APRS Space"}}},
	{ID: "us-beacons", Description: "NCDXF/IARU HF beacons, 20m to 10m (CW, receive only)"},
	{ID: "us-cb", Description: "Citizens Band channels 1-40, in AM, LSB, and USB"},
	{ID: "us-fm-sats", Description: "FM amateur satellites, with uplink tones (the ISS is in us-iss)"},
	{ID: "us-ft4", Description: "FT4 dial frequencies, 80m to 2m (USB)"},
	{ID: "us-ft8", Description: "FT8 dial frequencies, 160m to 2m (USB; 60m is in us-60m)"},
	{ID: "us-frs", Description: "FRS channels 1-22, with power limits"},
	{ID: "us-gmrs", Description: "GMRS channels 1-22 and repeater pairs 15R-22R (license required)"},
	{ID: "us-ham-calling", Description: "US amateur FM and SSB simplex calling frequencies"},
	{ID: "us-interop", Description: "National public-safety interoperability channels (receive only)"},
	{ID: "us-iss", Description: "International Space Station voice and cross-band repeater"},
	{ID: "us-js8", Description: "JS8Call dial frequencies, 160m to 2m (USB; 17m and 6m share FT4's, in us-ft4)"},
	{ID: "us-murs", Description: "MURS channels 1-5"},
	{ID: "us-noaa", Description: "NOAA Weather Radio WX1-WX7 (receive only)"},
	{ID: "us-psk31", Description: "PSK31 activity frequencies, 160m to 10m (USB; 17m shares FT8's, in us-ft8)"},
	{ID: "us-radiofax", Description: "NWS HF radiofax (weather fax) stations (USB, receive only)"},
	{ID: "us-sstv", Description: "Analog SSTV calling frequencies, 80m to 10m"},
	{ID: "us-time", Description: "WWV and CHU time signals (receive only)"},
	{ID: "us-wspr", Description: "WSPR dial frequencies, 160m to 23cm (USB)"},
}

func Lists() []List {
	out := append([]List(nil), lists...)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func Get(id string) (List, error) {
	for _, l := range lists {
		if l.ID == id {
			return l, nil
		}
	}
	ids := make([]string, len(lists))
	for i, l := range lists {
		ids[i] = l.ID
	}
	return List{}, fmt.Errorf("unknown list %q; available: %s", id, strings.Join(ids, ", "))
}

// Batch parses the list's channels. Built-in data is tested, so a row error
// here is a bug and is returned as an error.
func (l List) Batch() (importer.Batch, error) {
	f, err := files.Open("data/" + l.ID + ".csv")
	if err != nil {
		return importer.Batch{}, err
	}
	defer func() { _ = f.Close() }()
	b, err := importer.ParseGenericCSV(f)
	if err == nil && len(b.Errors) > 0 {
		err = fmt.Errorf("built-in list %s is invalid: %v", l.ID, b.Errors[0])
	}
	return b, err
}
