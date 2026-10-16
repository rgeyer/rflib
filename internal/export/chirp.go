package export

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"slices"

	"github.com/ryan/rflib/internal/chirp"
	"github.com/ryan/rflib/internal/domain"
)

// Validator is implemented by exporters that can't represent every channel,
// so callers can report or skip those channels before exporting.
type Validator interface {
	Validate(domain.Channel) error
}

// CHIRP writes CHIRP's generic CSV, numbering memories from Start in the
// order given.
type CHIRP struct {
	Start int
	// MaxNameLen truncates names to the radio's display width; 0 keeps them.
	MaxNameLen int
	// Locations, when set, gives each channel's memory number instead of
	// numbering from Start (radio exports use it for section starts).
	Locations []int
	// ToneModes and CrossModes, when set, are the CHIRP tone modes and
	// Cross modes the radio takes (see model.Model); a memory needing
	// another can't be written.
	ToneModes, CrossModes []string
}

func (x CHIRP) Validate(c domain.Channel) error {
	_, err := x.memory(c, 0)
	return err
}

// memory converts a channel, checking the radio takes its tone mode.
func (x CHIRP) memory(c domain.Channel, loc int) (chirp.Memory, error) {
	m, err := chirp.FromChannel(c, loc, x.MaxNameLen)
	if err != nil {
		return m, err
	}
	if len(x.ToneModes) > 0 && !slices.Contains(x.ToneModes, m.ToneMode) {
		if m.ToneMode == "Cross" {
			return m, fmt.Errorf("the radio can't hold different transmit and receive tones (CHIRP's Cross %s)", m.CrossMode)
		}
		return m, fmt.Errorf("the radio doesn't have tone mode %s", m.ToneMode)
	}
	if m.ToneMode == "Cross" && len(x.CrossModes) > 0 && !slices.Contains(x.CrossModes, m.CrossMode) {
		return m, fmt.Errorf("the radio doesn't have CHIRP's Cross mode %s", m.CrossMode)
	}
	return m, nil
}

// Export fails without writing anything if any channel can't be represented,
// naming every such channel.
func (x CHIRP) Export(w io.Writer, channels []domain.Channel) error {
	rows := make([][]string, 0, len(channels))
	var errs []error
	for i, c := range channels {
		loc := x.Start + i
		if x.Locations != nil {
			loc = x.Locations[i]
		}
		m, err := x.memory(c, loc)
		if err != nil {
			errs = append(errs, fmt.Errorf("channel %q (%s MHz): %w", c.Name, domain.FormatFrequencyMHz(c.ReceiveHz), err))
			continue
		}
		rows = append(rows, m.Record())
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	csvw := csv.NewWriter(w)
	csvw.UseCRLF = true // CHIRP writes CRLF; match it for Windows users
	if err := csvw.Write(chirp.Columns); err != nil {
		return fmt.Errorf("write CSV header: %w", err)
	}
	if err := csvw.WriteAll(rows); err != nil {
		return fmt.Errorf("write CSV rows: %w", err)
	}
	return nil
}
