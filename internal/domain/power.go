package domain

import (
	"slices"
	"strings"
)

// PowerIntent is how strongly a channel should transmit, independent of any
// radio: each radio model maps intents to its own power levels. Empty means
// no preference, so the radio's default applies.
type PowerIntent string

const (
	PowerUnset  PowerIntent = ""
	PowerLow    PowerIntent = "low"
	PowerMedium PowerIntent = "medium"
	PowerHigh   PowerIntent = "high"
	PowerMax    PowerIntent = "max"
)

var PowerIntents = []PowerIntent{PowerLow, PowerMedium, PowerHigh, PowerMax}

// ParsePowerIntent accepts an intent name, or empty for no preference.
func ParsePowerIntent(v string) (PowerIntent, error) {
	p := PowerIntent(strings.ToLower(strings.TrimSpace(v)))
	if p == PowerUnset || slices.Contains(PowerIntents, p) {
		return p, nil
	}
	return "", Invalidf("unknown power %q; use low, medium, high, or max (or empty for none)", v)
}

// PowerFromText reads power as other tools write it: intents and common
// level labels map to an intent, and anything else, including watts, gives
// no preference. Source watts describe the source's radio, not yours.
func PowerFromText(v string) PowerIntent {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "low", "lo", "l", "low1", "low2":
		return PowerLow
	case "medium", "med", "mid", "m", "mid1", "mid2":
		return PowerMedium
	case "high", "hi", "h":
		return PowerHigh
	case "max", "maximum", "full":
		return PowerMax
	}
	return PowerUnset
}

// ResolvePower picks the effective intent: the first one set, most specific
// first (radio override, channel, radio default).
func ResolvePower(intents ...PowerIntent) PowerIntent {
	for _, p := range intents {
		if p != PowerUnset {
			return p
		}
	}
	return PowerUnset
}

// PowerLevel is one of a radio's power settings as its programming software
// names it, with its output when known.
type PowerLevel struct {
	Label string
	Watts *float64
}

// ServiceCapWatts is the most a channel may transmit under its service's US
// rules, when the service limits power. It reports false for services
// without a fixed cap (amateur, marine, and so on) and receive-only
// channels.
func ServiceCapWatts(c Channel) (float64, bool) {
	if c.TransmitHz == nil || c.RXOnly {
		return 0, false
	}
	tx := *c.TransmitHz
	interstitial467 := tx >= 467_562_500 && tx <= 467_712_500 && (tx-467_562_500)%25_000 == 0
	interstitial462 := tx >= 462_562_500 && tx <= 462_712_500 && (tx-462_562_500)%25_000 == 0
	switch c.Service {
	case ServiceFRS:
		if interstitial467 {
			return 0.5, true // FRS 8-14
		}
		return 2, true
	case ServiceGMRS:
		switch {
		case interstitial467:
			return 0.5, true // GMRS 8-14
		case interstitial462:
			return 5, true // GMRS 1-7
		}
		return 50, true
	case ServiceMURS:
		return 2, true
	case ServiceCB:
		if c.Mode == "USB" || c.Mode == "LSB" {
			return 12, true // SSB, peak envelope power
		}
		return 4, true // AM carrier
	}
	return 0, false
}
