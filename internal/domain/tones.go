package domain

import "slices"

// StandardCTCSSMilliHz is the 50-tone EIA CTCSS set most radios and
// programming software (including CHIRP) accept. The library stores any tone
// from 50 to 300 Hz; exporters use this list to reject what a target can't
// program.
var StandardCTCSSMilliHz = []int64{
	67_000, 69_300, 71_900, 74_400, 77_000, 79_700, 82_500, 85_400, 88_500, 91_500,
	94_800, 97_400, 100_000, 103_500, 107_200, 110_900, 114_800, 118_800, 123_000, 127_300,
	131_800, 136_500, 141_300, 146_200, 151_400, 156_700, 159_800, 162_200, 165_500, 167_900,
	171_300, 173_800, 177_300, 179_900, 183_500, 186_200, 189_900, 192_800, 196_600, 199_500,
	203_500, 206_500, 210_700, 218_100, 225_700, 229_100, 233_600, 241_800, 250_300, 254_100,
}

// StandardDCSCodes is the 104-code standard DCS set, written as the decimal
// value of each octal code's digits (023 is 23), matching Tone.DCSCode.
var StandardDCSCodes = []int{
	23, 25, 26, 31, 32, 36, 43, 47, 51, 53, 54, 65, 71, 72, 73, 74,
	114, 115, 116, 122, 125, 131, 132, 134, 143, 145, 152, 155, 156, 162, 165, 172, 174,
	205, 212, 223, 225, 226, 243, 244, 245, 246, 251, 252, 255, 261, 263, 265, 266, 271, 274,
	306, 311, 315, 325, 331, 332, 343, 346, 351, 356, 364, 365, 371,
	411, 412, 413, 423, 431, 432, 445, 446, 452, 454, 455, 462, 464, 465, 466,
	503, 506, 516, 523, 526, 532, 546, 565,
	606, 612, 624, 627, 631, 632, 654, 662, 664,
	703, 712, 723, 731, 732, 734, 743, 754,
}

func IsStandardCTCSS(milliHz int64) bool { return slices.Contains(StandardCTCSSMilliHz, milliHz) }
func IsStandardDCS(code int) bool        { return slices.Contains(StandardDCSCodes, code) }

// IsStandardTone reports whether t is none or a standard CTCSS/DCS tone.
func IsStandardTone(t Tone) bool {
	switch t.Kind {
	case ToneCTCSS:
		return t.CTCSSMilliHz != nil && IsStandardCTCSS(*t.CTCSSMilliHz)
	case ToneDCS:
		return t.DCSCode != nil && IsStandardDCS(*t.DCSCode)
	}
	return true
}

// ProgrammedTones returns the tones a radio is programmed with: the transmit
// tone, and the receive tone only when tone squelch is on (otherwise the
// radio ignores it).
func ProgrammedTones(c Channel) (tx, rx Tone) {
	tx, rx = c.TransmitTone, Tone{Kind: ToneNone}
	if tx.Kind == "" {
		tx.Kind = ToneNone
	}
	if c.ToneSquelch {
		rx = c.ReceiveTone
	}
	return tx, rx
}

// Duplex classifies a channel's stored frequencies the way CHIRP does,
// whether or not it is receive-only: off (no transmit frequency), simplex,
// + or - (an offset), or split.
func Duplex(c Channel) string {
	switch tx := c.TransmitHz; {
	case tx == nil:
		return "off"
	case *tx == c.ReceiveHz:
		return "simplex"
	case max(*tx-c.ReceiveHz, c.ReceiveHz-*tx) >= SplitThresholdHz:
		return "split"
	case *tx > c.ReceiveHz:
		return "+"
	}
	return "-"
}
