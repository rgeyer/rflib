package profile

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"

	"github.com/ryan/rflib/internal/domain"
)

// Exporter writes channels in a profile's format. It implements
// export.Exporter and export.Validator.
type Exporter struct {
	Profile Profile
	// Start overrides Profile.Start when set.
	Start *int
	// Locations, when set, gives each channel's memory number instead of
	// numbering from the start; with Slots, gaps become empty memories.
	Locations []int
}

func (x Exporter) start() int {
	if x.Start != nil {
		return *x.Start
	}
	return x.Profile.start()
}

// cell renders one column for a channel.
func (x Exporter) cell(r row, col Column) (string, error) {
	v, err := r.raw(x.Profile, col)
	if err != nil {
		return "", err
	}
	if v != "" && col.Suffix != "" && isNumeric(col.Source) {
		v += col.Suffix
	}
	if len(col.Map) > 0 && col.Source != "const" {
		out, ok := col.Map[v]
		if !ok {
			out, ok = col.Map["*"]
		}
		if !ok {
			return "", unsupported(col, v)
		}
		v = out
	}
	if v == "" {
		v = col.Default
	}
	return v, nil
}

func isNumeric(source string) bool {
	switch source {
	case "rx_mhz", "tx_mhz", "offset_mhz", "ctcss_hz", "tx_ctcss_hz", "rx_ctcss_hz":
		return true
	}
	return false
}

func unsupported(col Column, v string) error {
	switch col.Source {
	case "tone_mode":
		return fmt.Errorf("%s (tone mode %q) is not supported by this profile", toneModeHelp[v], v)
	case "mode":
		return fmt.Errorf("mode %s is not supported by this profile", v)
	case "duplex":
		return fmt.Errorf("duplex %q is not supported by this profile", v)
	}
	return fmt.Errorf("column %s has no mapping for %q", col.Name, v)
}

func (x Exporter) record(c domain.Channel, location int) ([]string, error) {
	tx, rx := domain.ProgrammedTones(c)
	if x.Profile.StandardTonesOnly {
		for _, t := range []domain.Tone{tx, rx} {
			if !domain.IsStandardTone(t) {
				return nil, fmt.Errorf("tone %s is not a standard CTCSS/DCS tone", domain.FormatTone(t))
			}
		}
	}
	if (tx.Inverted || rx.Inverted) && !x.hasSource("dcs_polarity") {
		return nil, fmt.Errorf("inverted DCS is not supported by this profile")
	}
	r := row{c: c, location: location, tx: tx, rx: rx}
	rec := make([]string, len(x.Profile.Columns))
	for i, col := range x.Profile.Columns {
		v, err := x.cell(r, col)
		if err != nil {
			return nil, err
		}
		rec[i] = v
	}
	return rec, nil
}

func (x Exporter) hasSource(src string) bool {
	for _, c := range x.Profile.Columns {
		if c.Source == src {
			return true
		}
	}
	return false
}

func (x Exporter) Validate(c domain.Channel) error {
	_, err := x.record(c, x.start())
	return err
}

// Export fails without writing anything if any channel can't be
// represented, naming every such channel.
func (x Exporter) Export(w io.Writer, channels []domain.Channel) error {
	p := x.Profile
	if p.Slots > 0 && len(channels) > p.Slots {
		return fmt.Errorf("profile %s holds %d memories; %d channels selected", p.Name, p.Slots, len(channels))
	}
	var rows [][]string
	if p.Header {
		header := make([]string, len(p.Columns))
		for i, c := range p.Columns {
			header[i] = c.Name
		}
		rows = append(rows, header)
	}
	locs := x.Locations
	if locs == nil {
		locs = make([]int, len(channels))
		for i := range locs {
			locs[i] = x.start() + i
		}
	}
	var errs []error
	byLoc := make(map[int][]string, len(channels))
	for i, c := range channels {
		if p.Slots > 0 && (locs[i] < x.start() || locs[i] >= x.start()+p.Slots) {
			errs = append(errs, fmt.Errorf("channel %q: memory %d is outside the profile's %d memories", c.Name, locs[i], p.Slots))
			continue
		}
		rec, err := x.record(c, locs[i])
		if err != nil {
			errs = append(errs, fmt.Errorf("channel %q (%s MHz): %w", c.Name, domain.FormatFrequencyMHz(c.ReceiveHz), err))
			continue
		}
		byLoc[locs[i]] = rec
		if p.Slots == 0 {
			rows = append(rows, rec)
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	// Padded formats list every memory in order, empty or not.
	for loc := x.start(); loc < x.start()+p.Slots; loc++ {
		if rec, ok := byLoc[loc]; ok {
			rows = append(rows, rec)
			continue
		}
		rec := make([]string, len(p.Columns))
		for j, col := range p.Columns {
			rec[j] = col.Empty
			if col.Source == "location" {
				rec[j] = fmt.Sprint(loc)
			}
		}
		rows = append(rows, rec)
	}
	cw := csv.NewWriter(w)
	cw.UseCRLF = p.LineEnding == "crlf"
	if err := cw.WriteAll(rows); err != nil {
		return fmt.Errorf("write CSV: %w", err)
	}
	return nil
}
