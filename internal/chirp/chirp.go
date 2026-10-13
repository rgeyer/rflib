// Package chirp maps library channels to and from CHIRP's generic CSV memory
// format (chirp/drivers/generic_csv.py), in both directions so the mapping
// can be tested as a round trip.
package chirp

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/ryan/rflib/internal/domain"
)

// Modes are the library modes CHIRP export and import support: all of them,
// since CHIRP uses the same names.
var Modes = domain.Modes

// Columns is CHIRP's generic CSV header, in CHIRP's order.
var Columns = []string{"Location", "Name", "Frequency", "Duplex", "Offset", "Tone", "rToneFreq", "cToneFreq", "DtcsCode", "DtcsPolarity", "RxDtcsCode", "CrossMode", "Mode", "TStep", "Skip", "Power", "Comment", "URCALL", "RPT1CALL", "RPT2CALL", "DVCODE"}

// SplitThresholdHz is the RX/TX difference at or above which a channel is
// written as duplex "split" (Offset holds the TX frequency) rather than as a
// +/- offset.
const SplitThresholdHz = domain.SplitThresholdHz

// Defaults CHIRP writes for tone fields a memory doesn't use.
const (
	defaultCTCSS     = 88_500
	defaultDCS       = 23
	defaultPolarity  = "NN"
	defaultCrossMode = "Tone->Tone"
	defaultTStep     = "5.00"
)

// Memory is one CHIRP memory row with parsed values.
type Memory struct {
	Location     int
	Name         string
	FrequencyHz  int64
	Duplex       string // "", "+", "-", "split", or "off"
	OffsetHz     int64  // the TX frequency when Duplex is "split"
	ToneMode     string // "", "Tone", "TSQL", "DTCS", or "Cross"
	RToneMilliHz int64  // transmit CTCSS for Tone and Cross modes
	CToneMilliHz int64  // CTCSS for TSQL, and receive CTCSS for Cross
	DTCS         int    // transmit DCS (both directions in DTCS mode)
	RxDTCS       int    // receive DCS in Cross mode
	DTCSPolarity string // TX then RX polarity: N (normal) or R (reversed)
	CrossMode    string
	Mode         string
	TStep        string
	Skip         string
	Power        string
	Comment      string
}

// Record formats m as a CSV row matching Columns.
func (m Memory) Record() []string {
	return []string{
		strconv.Itoa(m.Location), m.Name, domain.FormatFrequencyMHz(m.FrequencyHz), m.Duplex, domain.FormatFrequencyMHz(m.OffsetHz),
		m.ToneMode, formatCTCSS(m.RToneMilliHz), formatCTCSS(m.CToneMilliHz), fmt.Sprintf("%03d", m.DTCS), m.DTCSPolarity,
		fmt.Sprintf("%03d", m.RxDTCS), m.CrossMode, m.Mode, m.TStep, m.Skip, m.Power, m.Comment, "", "", "", "",
	}
}

// formatCTCSS writes tenths of a hertz, as CHIRP does. Standard tones have no
// finer precision; FromChannel rejects any that would.
func formatCTCSS(milliHz int64) string {
	return fmt.Sprintf("%d.%d", milliHz/1000, milliHz%1000/100)
}

// FromChannel converts a channel to a CHIRP memory. It fails when CHIRP
// can't represent the channel: an unsupported mode or a non-standard tone.
func FromChannel(c domain.Channel, location, maxNameLen int) (Memory, error) {
	m := Memory{
		Location: location, Name: truncate(c.Name, maxNameLen), FrequencyHz: c.ReceiveHz,
		RToneMilliHz: defaultCTCSS, CToneMilliHz: defaultCTCSS, DTCS: defaultDCS, RxDTCS: defaultDCS,
		DTCSPolarity: defaultPolarity, CrossMode: defaultCrossMode, Mode: c.Mode, TStep: defaultTStep,
		Power: chirpPower(c.ProgrammedPower), Comment: strings.Join(strings.Fields(c.Notes), " "),
	}
	if !slices.Contains(Modes, c.Mode) {
		return m, fmt.Errorf("mode %s is not supported by CHIRP export", c.Mode)
	}
	if c.Skip {
		m.Skip = "S"
	}
	tx := c.TransmitHz
	if c.RXOnly {
		tx = nil
	}
	m.Duplex, m.OffsetHz = duplex(c.ReceiveHz, tx)

	txTone, rxTone := domain.ProgrammedTones(c)
	for _, t := range []domain.Tone{txTone, rxTone} {
		if !domain.IsStandardTone(t) {
			return m, fmt.Errorf("tone %s is not a standard CTCSS/DCS tone CHIRP accepts", domain.FormatTone(t))
		}
	}
	setTones(&m, txTone, rxTone)
	return m, nil
}

// chirpPower writes a radio power level as CHIRP's CSV import reads it:
// watts. A level without known watts, or no level at all, is left blank and
// CHIRP picks its default.
func chirpPower(p *domain.PowerLevel) string {
	if p == nil || p.Watts == nil {
		return ""
	}
	return strconv.FormatFloat(*p.Watts, 'f', -1, 64) + "W"
}

func duplex(rx int64, tx *int64) (string, int64) {
	switch {
	case tx == nil:
		return "off", 0
	case *tx == rx:
		return "", 0
	case abs(*tx-rx) >= SplitThresholdHz:
		return "split", *tx
	case *tx > rx:
		return "+", *tx - rx
	default:
		return "-", rx - *tx
	}
}

// setTones chooses CHIRP's tone mode for a transmit tone and an effective
// receive (squelch) tone, using Cross whenever the simple modes can't
// express the pair.
func setTones(m *Memory, tx, rx domain.Tone) {
	switch {
	case tx.Kind == domain.ToneNone && rx.Kind == domain.ToneNone:
		return
	case tx.Kind == domain.ToneCTCSS && rx.Kind == domain.ToneNone:
		m.ToneMode, m.RToneMilliHz = "Tone", *tx.CTCSSMilliHz
		return
	case tx.Kind == domain.ToneCTCSS && rx.Kind == domain.ToneCTCSS && *tx.CTCSSMilliHz == *rx.CTCSSMilliHz:
		m.ToneMode, m.RToneMilliHz, m.CToneMilliHz = "TSQL", *tx.CTCSSMilliHz, *tx.CTCSSMilliHz
		return
	case tx.Kind == domain.ToneDCS && rx.Kind == domain.ToneDCS && *tx.DCSCode == *rx.DCSCode:
		m.ToneMode, m.DTCS, m.RxDTCS = "DTCS", *tx.DCSCode, *rx.DCSCode
		m.DTCSPolarity = polarity(tx) + polarity(rx)
		return
	}
	m.ToneMode = "Cross"
	m.CrossMode = crossPart(tx) + "->" + crossPart(rx)
	switch tx.Kind {
	case domain.ToneCTCSS:
		m.RToneMilliHz = *tx.CTCSSMilliHz
	case domain.ToneDCS:
		m.DTCS = *tx.DCSCode
	}
	switch rx.Kind {
	case domain.ToneCTCSS:
		m.CToneMilliHz = *rx.CTCSSMilliHz
	case domain.ToneDCS:
		m.RxDTCS = *rx.DCSCode
	}
	m.DTCSPolarity = polarity(tx) + polarity(rx)
}

func crossPart(t domain.Tone) string {
	switch t.Kind {
	case domain.ToneCTCSS:
		return "Tone"
	case domain.ToneDCS:
		return "DTCS"
	}
	return ""
}

func polarity(t domain.Tone) string {
	if t.Kind == domain.ToneDCS && t.Inverted {
		return "R"
	}
	return "N"
}

// ToChannel converts a CHIRP memory to a channel. A memory without a name is
// named after its frequency, since the library requires names.
func ToChannel(m Memory) (domain.Channel, error) {
	c := domain.Channel{Name: m.Name, ReceiveHz: m.FrequencyHz, Mode: m.Mode, Power: domain.PowerFromText(m.Power), Notes: m.Comment, Skip: m.Skip == "S" || m.Skip == "P"}
	if strings.TrimSpace(c.Name) == "" {
		c.Name = domain.FormatFrequencyMHz(m.FrequencyHz)
	}
	if !slices.Contains(Modes, m.Mode) {
		return c, fmt.Errorf("mode %q is not supported yet", m.Mode)
	}
	switch m.Duplex {
	case "":
		c.TransmitHz = &m.FrequencyHz
	case "+", "-", "split":
		tx := m.OffsetHz // split: Offset is the TX frequency
		switch m.Duplex {
		case "+":
			tx = m.FrequencyHz + m.OffsetHz
		case "-":
			tx = m.FrequencyHz - m.OffsetHz
		}
		if tx <= 0 {
			return c, fmt.Errorf("duplex %s with offset %s gives no valid transmit frequency", m.Duplex, domain.FormatFrequencyMHz(m.OffsetHz))
		}
		c.TransmitHz = &tx
	case "off":
	default:
		return c, fmt.Errorf("unknown duplex %q", m.Duplex)
	}

	ctcss := func(milliHz int64) domain.Tone { return domain.Tone{Kind: domain.ToneCTCSS, CTCSSMilliHz: &milliHz} }
	dcs := func(code int, pol byte) domain.Tone {
		return domain.Tone{Kind: domain.ToneDCS, DCSCode: &code, Inverted: pol == 'R'}
	}
	none := domain.Tone{Kind: domain.ToneNone}
	pol := m.DTCSPolarity + "NN" // tolerate a missing or short polarity

	tx, rx := none, none
	switch m.ToneMode {
	case "":
	case "Tone":
		tx = ctcss(m.RToneMilliHz)
	case "TSQL":
		tx, rx = ctcss(m.CToneMilliHz), ctcss(m.CToneMilliHz)
	case "DTCS":
		tx, rx = dcs(m.DTCS, pol[0]), dcs(m.DTCS, pol[1])
	case "Cross":
		parts := strings.Split(m.CrossMode, "->")
		if len(parts) != 2 {
			return c, fmt.Errorf("unknown cross mode %q", m.CrossMode)
		}
		var err error
		if tx, err = crossTone(parts[0], ctcss(m.RToneMilliHz), dcs(m.DTCS, pol[0])); err != nil {
			return c, err
		}
		if rx, err = crossTone(parts[1], ctcss(m.CToneMilliHz), dcs(m.RxDTCS, pol[1])); err != nil {
			return c, err
		}
	default:
		return c, fmt.Errorf("tone mode %q is not supported", m.ToneMode)
	}
	c.TransmitTone, c.ReceiveTone = tx, rx
	c.ToneSquelch = rx.Kind != domain.ToneNone
	return c, nil
}

func crossTone(part string, ctcss, dcs domain.Tone) (domain.Tone, error) {
	switch part {
	case "":
		return domain.Tone{Kind: domain.ToneNone}, nil
	case "Tone":
		return ctcss, nil
	case "DTCS":
		return dcs, nil
	}
	return domain.Tone{}, fmt.Errorf("unknown cross mode part %q", part)
}

// ParseRecord reads a memory from a CSV row; get returns a column's trimmed
// value by CHIRP header name, or "" when the column is absent. Missing tone
// fields fall back to CHIRP's defaults.
func ParseRecord(get func(string) string) (Memory, error) {
	m := Memory{Name: get("Name"), Duplex: get("Duplex"), ToneMode: get("Tone"), DTCSPolarity: get("DtcsPolarity"), CrossMode: get("CrossMode"),
		Mode: strings.ToUpper(get("Mode")), TStep: get("TStep"), Skip: get("Skip"), Power: get("Power"), Comment: get("Comment")}
	var err error
	if m.Location, err = optionalInt(get("Location"), 0); err != nil {
		return m, fmt.Errorf("column Location: %w", err)
	}
	if m.FrequencyHz, err = domain.ParseFrequencyMHz(get("Frequency")); err != nil {
		return m, fmt.Errorf("column Frequency: %w", err)
	}
	if v := get("Offset"); v != "" {
		if m.OffsetHz, err = parseMHz(v); err != nil {
			return m, fmt.Errorf("column Offset: %w", err)
		}
	}
	for _, f := range []struct {
		col string
		dst *int64
	}{{"rToneFreq", &m.RToneMilliHz}, {"cToneFreq", &m.CToneMilliHz}} {
		*f.dst = defaultCTCSS
		if v := get(f.col); v != "" {
			t, err := domain.ParseTone("ctcss:" + v)
			if err != nil {
				return m, fmt.Errorf("column %s: %w", f.col, err)
			}
			*f.dst = *t.CTCSSMilliHz
		}
	}
	for _, f := range []struct {
		col string
		dst *int
	}{{"DtcsCode", &m.DTCS}, {"RxDtcsCode", &m.RxDTCS}} {
		*f.dst = defaultDCS
		if v := get(f.col); v != "" {
			t, err := domain.ParseTone("dcs:" + v)
			if err != nil {
				return m, fmt.Errorf("column %s: %w", f.col, err)
			}
			*f.dst = *t.DCSCode
		}
	}
	// Older CHIRP files have no RxDtcsCode; the transmit code applied to both.
	if get("RxDtcsCode") == "" {
		m.RxDTCS = m.DTCS
	}
	return m, nil
}

// parseMHz accepts a zero offset, which domain.ParseFrequencyMHz rejects.
func parseMHz(v string) (int64, error) {
	if f, err := strconv.ParseFloat(v, 64); err == nil && f == 0 {
		return 0, nil
	}
	return domain.ParseFrequencyMHz(v)
}

func optionalInt(v string, fallback int) (int, error) {
	if v == "" {
		return fallback, nil
	}
	return strconv.Atoi(v)
}

func truncate(s string, max int) string {
	r := []rune(s)
	if max <= 0 || len(r) <= max {
		return s
	}
	return strings.TrimSpace(string(r[:max]))
}

func abs(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}
