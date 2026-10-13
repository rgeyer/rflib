package importer

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/ryan/rflib/internal/domain"
)

// ParseGenericCSV reads the format written by export.GenericCSV. Columns are
// matched by header name, so order doesn't matter and unknown columns are
// ignored; only name and receive_mhz are required. Row numbers are file line
// numbers.
func ParseGenericCSV(r io.Reader) (Batch, error) {
	var b Batch
	err := readCSV(r, []string{"name", "receive_mhz"}, func(row int, get func(string) string) {
		if c, err := channelFromCSV(get); err != nil {
			b.fail(row, "%v", err)
		} else {
			b.finish(row, c)
		}
	}, &b)
	return b, err
}

// readCSV calls fn for each data row with its line number and a lookup of
// trimmed values by lowercase header name. Malformed rows become row errors
// in b; only an unreadable header or I/O failure is returned.
func readCSV(r io.Reader, required []string, fn func(row int, get func(string) string), b *Batch) error {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	header, err := cr.Read()
	if err != nil {
		return fmt.Errorf("read CSV header: %w", err)
	}
	col := make(map[string]int, len(header))
	for i, h := range header {
		col[lower(strings.TrimPrefix(h, byteOrderMark))] = i
	}
	for _, name := range required {
		if _, ok := col[name]; !ok {
			return fmt.Errorf("CSV header is missing the %q column", name)
		}
	}
	for {
		fields, err := cr.Read()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			var pe *csv.ParseError
			if errors.As(err, &pe) {
				b.fail(pe.StartLine, "%v", pe.Err)
				continue
			}
			return fmt.Errorf("read CSV: %w", err)
		}
		row, _ := cr.FieldPos(0)
		fn(row, func(name string) string {
			if i, ok := col[name]; ok && i < len(fields) {
				return strings.TrimSpace(fields[i])
			}
			return ""
		})
	}
}

func lower(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

func channelFromCSV(get func(string) string) (domain.Channel, error) {
	c := domain.Channel{ID: get("id"), Name: get("name"), Mode: get("mode"), Power: get("power"), Bandwidth: get("bandwidth"), Notes: get("notes")}
	var err error
	if c.ReceiveHz, err = domain.ParseFrequencyMHz(get("receive_mhz")); err != nil {
		return c, fmt.Errorf("receive_mhz: %w", err)
	}
	if v := get("transmit_mhz"); v != "" {
		hz, err := domain.ParseFrequencyMHz(v)
		if err != nil {
			return c, fmt.Errorf("transmit_mhz: %w", err)
		}
		c.TransmitHz = &hz
	}
	if c.ReceiveTone, err = domain.ParseTone(get("receive_tone")); err != nil {
		return c, fmt.Errorf("receive_tone: %w", err)
	}
	if c.TransmitTone, err = domain.ParseTone(get("transmit_tone")); err != nil {
		return c, fmt.Errorf("transmit_tone: %w", err)
	}
	if c.ToneSquelch, err = parseBool(get("tone_squelch")); err != nil {
		return c, fmt.Errorf("tone_squelch: %w", err)
	}
	if c.Skip, err = parseBool(get("skip")); err != nil {
		return c, fmt.Errorf("skip: %w", err)
	}
	if c.Service, err = domain.ParseService(get("service")); err != nil {
		return c, fmt.Errorf("service: %w", err)
	}
	if c.RXOnly, err = parseBool(get("rx_only")); err != nil {
		return c, fmt.Errorf("rx_only: %w", err)
	}
	if v := get("tags"); v != "" {
		c.Tags = strings.Split(v, ";")
	}
	if id, name := get("group_id"), get("group_name"); id != "" || name != "" {
		g := domain.Group{ID: id, Name: name}
		if g.Latitude, err = parseOptionalFloat(get("group_latitude")); err != nil {
			return c, fmt.Errorf("group_latitude: %w", err)
		}
		if g.Longitude, err = parseOptionalFloat(get("group_longitude")); err != nil {
			return c, fmt.Errorf("group_longitude: %w", err)
		}
		c.Group = &g
	}
	return c, nil
}

func parseBool(v string) (bool, error) {
	if v == "" {
		return false, nil
	}
	return strconv.ParseBool(v)
}

func parseOptionalFloat(v string) (*float64, error) {
	if v == "" {
		return nil, nil
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return nil, err
	}
	return &f, nil
}

// byteOrderMark is stripped from the first header so spreadsheet exports parse.
const byteOrderMark = "\xef\xbb\xbf"
