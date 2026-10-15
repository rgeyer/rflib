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
}

// lists describes every file in data/; TestEveryListIsDescribed keeps them in
// step.
var lists = []List{
	{"us-60m", "US amateur 60m channels 1-5 (USB dial frequencies)"},
	{"us-aprs", "APRS: the national frequency and the space digipeaters"},
	{"us-cb", "Citizens Band channels 1-40 (AM)"},
	{"us-ft8", "FT8 dial frequencies, 160m to 2m (USB; 60m is in us-60m)"},
	{"us-frs", "FRS channels 1-22, with power limits"},
	{"us-gmrs", "GMRS channels 1-22 and repeater pairs 15R-22R (license required)"},
	{"us-ham-calling", "US amateur FM and SSB simplex calling frequencies"},
	{"us-interop", "National public-safety interoperability channels (receive only)"},
	{"us-iss", "International Space Station voice and cross-band repeater"},
	{"us-murs", "MURS channels 1-5"},
	{"us-noaa", "NOAA Weather Radio WX1-WX7 (receive only)"},
	{"us-time", "WWV and CHU time signals (receive only)"},
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
