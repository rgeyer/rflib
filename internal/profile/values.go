package profile

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/ryan/rflib/internal/domain"
)

// splitThresholdHz: offsets this large are written as split rather than +/-.
const splitThresholdHz = domain.SplitThresholdHz

// ToneMode names the programmed tone combination of a channel.
func ToneMode(tx, rx domain.Tone) string {
	k := func(t domain.Tone) domain.ToneKind { return t.Kind }
	switch {
	case k(tx) == domain.ToneNone && k(rx) == domain.ToneNone:
		return "none"
	case k(tx) == domain.ToneCTCSS && k(rx) == domain.ToneNone:
		return "tone"
	case k(tx) == domain.ToneCTCSS && k(rx) == domain.ToneCTCSS:
		if *tx.CTCSSMilliHz == *rx.CTCSSMilliHz {
			return "tsql"
		}
		return "tone_tone"
	case k(tx) == domain.ToneDCS && k(rx) == domain.ToneDCS:
		if *tx.DCSCode == *rx.DCSCode {
			return "dcs"
		}
		return "dcs_dcs"
	case k(tx) == domain.ToneDCS && k(rx) == domain.ToneNone:
		return "dcs_enc"
	case k(tx) == domain.ToneCTCSS && k(rx) == domain.ToneDCS:
		return "tone_dcs"
	case k(tx) == domain.ToneDCS && k(rx) == domain.ToneCTCSS:
		return "dcs_tone"
	case k(tx) == domain.ToneNone && k(rx) == domain.ToneCTCSS:
		return "rx_tone"
	default:
		return "rx_dcs"
	}
}

// toneModeHelp describes tone modes in validation errors.
var toneModeHelp = map[string]string{
	"tone": "tone encode", "tsql": "tone squelch", "dcs": "DCS", "dcs_enc": "DCS encode only",
	"tone_dcs": "CTCSS transmit with DCS squelch", "dcs_tone": "DCS transmit with CTCSS squelch",
	"tone_tone": "different transmit and receive CTCSS tones", "dcs_dcs": "different transmit and receive DCS codes",
	"rx_tone": "CTCSS squelch without a transmit tone", "rx_dcs": "DCS squelch without a transmit code",
}

// row is a channel's raw source values for one memory location.
type row struct {
	c        domain.Channel
	location int
	tx, rx   domain.Tone
}

// raw returns a source's value before formatting. Numeric sources return
// the integer value as a string; format applies decimals and suffix.
func (r row) raw(p Profile, col Column) (string, error) {
	c := r.c
	txHz := c.ReceiveHz
	if c.TransmitHz != nil && !c.RXOnly {
		txHz = *c.TransmitHz
	}
	switch col.Source {
	case "location":
		return strconv.Itoa(r.location), nil
	case "name":
		name := c.Name
		if p.NameCase == "upper" {
			name = strings.ToUpper(name)
		}
		if runes := []rune(name); p.MaxNameLength > 0 && len(runes) > p.MaxNameLength {
			name = strings.TrimSpace(string(runes[:p.MaxNameLength]))
		}
		return name, nil
	case "rx_mhz":
		return formatHz(c.ReceiveHz, col)
	case "tx_mhz":
		return formatHz(txHz, col)
	case "offset_mhz":
		d := r.duplex()
		if d != "plus" && d != "minus" {
			return formatHz(0, col)
		}
		return formatHz(abs(txHz-c.ReceiveHz), col)
	case "duplex":
		return r.duplex(), nil
	case "mode":
		return c.Mode, nil
	case "narrow":
		return strconv.FormatBool(c.Mode == "NFM"), nil
	case "tone_mode":
		return ToneMode(r.tx, r.rx), nil
	case "ctcss_hz":
		return formatCTCSS(first(r.tx, r.rx, domain.ToneCTCSS), col), nil
	case "tx_ctcss_hz":
		return formatCTCSS(only(r.tx, domain.ToneCTCSS), col), nil
	case "rx_ctcss_hz":
		return formatCTCSS(only(r.rx, domain.ToneCTCSS), col), nil
	case "dcs_code":
		return formatDCS(first(r.tx, r.rx, domain.ToneDCS)), nil
	case "tx_dcs_code":
		return formatDCS(only(r.tx, domain.ToneDCS)), nil
	case "rx_dcs_code":
		return formatDCS(only(r.rx, domain.ToneDCS)), nil
	case "dcs_polarity":
		pol := func(t domain.Tone) string {
			if t.Kind == domain.ToneDCS && t.Inverted {
				return "R"
			}
			return "N"
		}
		return pol(r.tx) + pol(r.rx), nil
	case "power":
		if c.ProgrammedPower != nil {
			return strings.ToUpper(c.ProgrammedPower.Label), nil
		}
		return strings.ToUpper(string(c.Power)), nil
	case "skip":
		return strconv.FormatBool(c.Skip), nil
	case "notes":
		return strings.Join(strings.Fields(c.Notes), " "), nil
	case "service":
		return string(c.Service), nil
	case "tags":
		return strings.Join(c.Tags, ";"), nil
	case "step_khz":
		for _, s := range col.StepsKHz {
			if milliHz, _ := parseKHz(s); c.ReceiveHz*1000%milliHz == 0 {
				return s, nil
			}
		}
		return "", fmt.Errorf("receive frequency %s MHz isn't a multiple of any step in %v kHz", domain.FormatFrequencyMHz(c.ReceiveHz), col.StepsKHz)
	case "const":
		return col.Value, nil
	}
	return "", fmt.Errorf("unknown source %q", col.Source)
}

func (r row) duplex() string {
	c := r.c
	switch {
	case c.TransmitHz == nil || c.RXOnly:
		return "off"
	case *c.TransmitHz == c.ReceiveHz:
		return "none"
	case abs(*c.TransmitHz-c.ReceiveHz) >= splitThresholdHz:
		return "split"
	case *c.TransmitHz > c.ReceiveHz:
		return "plus"
	default:
		return "minus"
	}
}

// first returns the transmit tone if it is of kind k, else the receive tone
// if it is, else none.
func first(tx, rx domain.Tone, k domain.ToneKind) domain.Tone {
	if tx.Kind == k {
		return tx
	}
	return only(rx, k)
}

func only(t domain.Tone, k domain.ToneKind) domain.Tone {
	if t.Kind == k {
		return t
	}
	return domain.Tone{Kind: domain.ToneNone}
}

func decimals(col Column, fallback int) int {
	if col.Decimals != nil {
		return *col.Decimals
	}
	return fallback
}

// formatHz writes hz as MHz with the column's decimals (default 6), failing
// rather than rounding when the frequency needs more digits.
func formatHz(hz int64, col Column) (string, error) {
	d := decimals(col, 6)
	full := domain.FormatFrequencyMHz(hz) // always 6 decimals
	cut := len(full) - (6 - d)
	if strings.Trim(full[cut:], "0") != "" {
		return "", fmt.Errorf("%s MHz needs more than %d decimals for column %s", full, d, col.Name)
	}
	return strings.TrimSuffix(full[:cut], "."), nil
}

// formatCTCSS writes a tone in Hz with the column's decimals (default 1).
// Precision beyond that is cut, which standard tones never need.
func formatCTCSS(t domain.Tone, col Column) string {
	if t.Kind != domain.ToneCTCSS {
		return ""
	}
	d := decimals(col, 1)
	s := fmt.Sprintf("%d.%03d", *t.CTCSSMilliHz/1000, *t.CTCSSMilliHz%1000)
	return strings.TrimSuffix(s[:len(s)-(3-d)], ".")
}

func formatDCS(t domain.Tone) string {
	if t.Kind != domain.ToneDCS {
		return ""
	}
	return fmt.Sprintf("%03d", *t.DCSCode)
}

// parseKHz parses a step such as "6.25" into millihertz.
func parseKHz(s string) (int64, error) {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || f <= 0 {
		return 0, fmt.Errorf("must be a positive number of kHz")
	}
	scaled := f * 1_000_000
	milliHz := int64(math.Round(scaled))
	if math.Abs(scaled-float64(milliHz)) > 1e-6 {
		return 0, fmt.Errorf("must be a whole number of millihertz")
	}
	return milliHz, nil
}

func abs(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}
