package profile

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/ryan/rflib/internal/domain"
	"github.com/ryan/rflib/internal/importer"
)

// Parser reads a profile's CSV format back into channels. Memory locations
// are not kept, and rows without a receive frequency (empty memories) are
// ignored.
type Parser struct{ Profile Profile }

func (x Parser) Parse(r io.Reader) (importer.Batch, error) {
	var b importer.Batch
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	if x.Profile.Header {
		if _, err := cr.Read(); err != nil {
			return b, fmt.Errorf("read CSV header: %w", err)
		}
	}
	for {
		fields, err := cr.Read()
		if errors.Is(err, io.EOF) {
			return b, nil
		}
		if err != nil {
			var pe *csv.ParseError
			if errors.As(err, &pe) {
				b.Fail(pe.StartLine, pe.Err)
				continue
			}
			return b, fmt.Errorf("read CSV: %w", err)
		}
		line, _ := cr.FieldPos(0)
		get, err := x.lookup(fields)
		if err != nil {
			b.Fail(line, err)
			continue
		}
		if get("rx_mhz") == "" {
			continue
		}
		c, err := x.channel(get)
		if err != nil {
			b.Fail(line, err)
			continue
		}
		b.Add(line, c)
	}
}

// lookup returns a function giving each source's library value in a row:
// the cell text with the suffix stripped and the column map reversed. It
// fails on text the map doesn't know.
func (x Parser) lookup(fields []string) (func(string) string, error) {
	values := map[string]string{}
	for i, col := range x.Profile.Columns {
		if ignoredOnImport[col.Source] || i >= len(fields) {
			continue
		}
		if _, seen := values[col.Source]; seen {
			continue
		}
		v := strings.TrimSpace(fields[i])
		v = strings.TrimSpace(strings.TrimSuffix(v, col.Suffix))
		if v != "" && len(col.Map) > 0 {
			lib, ok := reverse(col.Map, v)
			if !ok {
				return nil, fmt.Errorf("column %s: unknown value %q", col.Name, v)
			}
			v = lib
		}
		values[col.Source] = v
	}
	return func(src string) string { return values[src] }, nil
}

// ignoredOnImport are sources that don't become channel data: memory
// numbering, tuning step, and fixed values.
var ignoredOnImport = map[string]bool{"location": true, "step_khz": true, "const": true}

// reverse finds the library value a map writes as text. When several do,
// the one spelled the same as the text wins, then the alphabetically first;
// text the map doesn't list passes through when the map has a "*" entry.
func reverse(m map[string]string, text string) (string, bool) {
	var keys []string
	for k, v := range m {
		if k != "*" && strings.EqualFold(v, text) {
			keys = append(keys, k)
		}
	}
	if len(keys) == 0 {
		_, wildcard := m["*"]
		return text, wildcard
	}
	if slices.Contains(keys, text) {
		return text, true
	}
	slices.Sort(keys)
	return keys[0], true
}

func (x Parser) has(src string) bool { return Exporter{Profile: x.Profile}.hasSource(src) }

func (x Parser) channel(get func(string) string) (domain.Channel, error) {
	c := domain.Channel{Name: get("name"), Mode: strings.ToUpper(get("mode")), Power: get("power"), Notes: get("notes"), Skip: get("skip") == "true"}
	var err error
	if c.ReceiveHz, err = domain.ParseFrequencyMHz(get("rx_mhz")); err != nil {
		return c, fmt.Errorf("receive frequency: %w", err)
	}
	if c.Name == "" {
		c.Name = domain.FormatFrequencyMHz(c.ReceiveHz)
	}
	if c.Mode == "" {
		c.Mode = "FM"
	}
	if c.Mode == "FM" && get("narrow") == "true" {
		c.Mode = "NFM"
	}
	if c.TransmitHz, err = x.transmit(get, c.ReceiveHz); err != nil {
		return c, err
	}
	if c.TransmitTone, c.ReceiveTone, err = x.tones(get); err != nil {
		return c, err
	}
	c.ToneSquelch = c.ReceiveTone.Kind != domain.ToneNone
	if c.Service, err = domain.ParseService(get("service")); err != nil {
		return c, err
	}
	if t := get("tags"); t != "" {
		c.Tags = strings.Split(t, ";")
	}
	if g := get("group"); g != "" {
		c.Group = &domain.Group{Name: g}
	}
	return c, nil
}

func (x Parser) transmit(get func(string) string, rx int64) (*int64, error) {
	duplex := get("duplex")
	if duplex == "off" {
		return nil, nil
	}
	if x.has("tx_mhz") && get("tx_mhz") != "" {
		tx, err := domain.ParseFrequencyMHz(get("tx_mhz"))
		if err != nil {
			return nil, fmt.Errorf("transmit frequency: %w", err)
		}
		return &tx, nil
	}
	offset := int64(0)
	if v := get("offset_mhz"); v != "" && strings.Trim(v, "0.") != "" {
		var err error
		if offset, err = domain.ParseFrequencyMHz(v); err != nil {
			return nil, fmt.Errorf("offset: %w", err)
		}
	}
	tx := rx
	switch duplex {
	case "", "none":
	case "plus":
		tx = rx + offset
	case "minus":
		tx = rx - offset
	default:
		return nil, fmt.Errorf("duplex %q needs a transmit frequency column", duplex)
	}
	return &tx, nil
}

func (x Parser) tones(get func(string) string) (tx, rx domain.Tone, err error) {
	none := domain.Tone{Kind: domain.ToneNone}
	pick := func(specific, general string) string {
		if v := get(specific); v != "" {
			return v
		}
		return get(general)
	}
	pol := get("dcs_polarity") + "NN"
	ctcss := func(src, general string) (domain.Tone, error) {
		return domain.ParseTone("ctcss:" + pick(src, general))
	}
	dcs := func(src, general string, p byte) (domain.Tone, error) {
		v := "dcs:" + pick(src, general)
		if p == 'R' {
			v += ":inverted"
		}
		return domain.ParseTone(v)
	}
	both := func(a, b func() (domain.Tone, error)) (domain.Tone, domain.Tone, error) {
		t1, err := a()
		if err != nil {
			return t1, none, err
		}
		t2, err := b()
		return t1, t2, err
	}
	txC := func() (domain.Tone, error) { return ctcss("tx_ctcss_hz", "ctcss_hz") }
	rxC := func() (domain.Tone, error) { return ctcss("rx_ctcss_hz", "ctcss_hz") }
	txD := func() (domain.Tone, error) { return dcs("tx_dcs_code", "dcs_code", pol[0]) }
	rxD := func() (domain.Tone, error) { return dcs("rx_dcs_code", "dcs_code", pol[1]) }
	off := func() (domain.Tone, error) { return none, nil }

	mode := get("tone_mode")
	switch mode {
	case "", "none":
		return none, none, nil
	case "tone":
		tx, rx, err = both(txC, off)
	case "tsql": // one tone both ways, whichever column holds it
		tx, rx, err = both(txC, txC)
	case "tone_tone":
		tx, rx, err = both(txC, rxC)
	case "dcs": // one code both ways; polarity can still differ
		tx, rx, err = both(txD, func() (domain.Tone, error) { return dcs("tx_dcs_code", "dcs_code", pol[1]) })
	case "dcs_dcs":
		tx, rx, err = both(txD, rxD)
	case "dcs_enc":
		tx, rx, err = both(txD, off)
	case "tone_dcs":
		tx, rx, err = both(txC, rxD)
	case "dcs_tone":
		tx, rx, err = both(txD, rxC)
	case "rx_tone":
		tx, rx, err = both(off, rxC)
	case "rx_dcs":
		tx, rx, err = both(off, rxD)
	default:
		return none, none, fmt.Errorf("unknown tone mode %q", mode)
	}
	if err != nil {
		err = fmt.Errorf("tone mode %s: %w", mode, err)
	}
	return tx, rx, err
}
