package domain

import (
	"crypto/rand"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Modes are the analog modulation modes the library stores.
var Modes = []string{"FM", "NFM", "AM", "USB", "LSB"}

type ToneKind string

const (
	ToneNone  ToneKind = "none"
	ToneCTCSS ToneKind = "ctcss"
	ToneDCS   ToneKind = "dcs"
)

type Tone struct {
	Kind         ToneKind
	CTCSSMilliHz *int64
	DCSCode      *int
	Inverted     bool
}

// Location is where a channel is: a point, or with RadiusKM a circle, such
// as an approximate position or a repeater's coverage.
type Location struct {
	Latitude  float64
	Longitude float64
	RadiusKM  *float64
}

type Channel struct {
	ID         string
	Name       string
	ReceiveHz  int64
	TransmitHz *int64
	Mode       string
	Service    Service
	// RXOnly marks a channel never to transmit on, even when TransmitHz
	// records the system's input frequency; exporters program it receive
	// only.
	RXOnly       bool
	ReceiveTone  Tone
	TransmitTone Tone
	ToneSquelch  bool
	Power        PowerIntent
	Bandwidth    string
	Skip         bool
	Notes        string
	Tags         []string
	// ShortNames are the user's own abbreviations of Name, for radios with
	// narrow displays (see package naming).
	ShortNames []string
	// Location is where the channel is used, if known (see query near:).
	Location *Location
	// Networks are the channel's nodes on linking networks, such as its
	// AllStar node.
	Networks []NetworkID
	// Orgs are the organizations that operate or support a shared
	// channel; private channels have none.
	Orgs []OrgRef
	// Status says whether the channel is on the air. Only shared channels
	// change it; empty means active.
	Status Status
	// StatusNote explains the status, such as "down for antenna work".
	StatusNote string
	// BackOn is when an off-air channel is expected back, if known.
	BackOn *time.Time
	// ReplacedBy is the shared channel a retired one gave way to.
	ReplacedBy string
	// Revision counts a shared channel's changes (see Revision); zero for
	// private channels.
	Revision int
	// Link, when set, makes the channel a link to a shared channel: every
	// field but ID, Tags, Notes, Skip, Power, ShortNames, and the
	// timestamps comes from the shared channel, and follows its changes.
	Link *Link
	// CopiedFrom is the shared channel a private copy was made from.
	CopiedFrom string
	// ProgrammedPower is the radio power level chosen for an export. It is
	// set on export copies by a radio's power resolution and never stored.
	ProgrammedPower *PowerLevel
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func NewID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate ID: %w", err)
	}
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:]), nil
}

func ParseFrequencyMHz(value string) (int64, error) {
	hz, err := parseDecimalUnits(value, 6)
	if err != nil || hz <= 0 {
		return 0, Invalidf("frequency must be a positive MHz value with at most 6 decimal places")
	}
	return hz, nil
}

func FormatFrequencyMHz(hz int64) string { return formatDecimalUnits(hz, 6) }

func parseDecimalUnits(value string, places int) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" || strings.HasPrefix(value, "-") || strings.HasPrefix(value, "+") {
		return 0, Invalidf("must be an unsigned decimal")
	}
	parts := strings.Split(value, ".")
	if len(parts) > 2 || parts[0] == "" || len(parts) == 2 && len(parts[1]) > places {
		return 0, Invalidf("invalid decimal")
	}
	whole, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || whole < 0 {
		return 0, Invalidf("invalid whole number")
	}
	fraction := ""
	if len(parts) == 2 {
		fraction = parts[1]
	}
	if fraction != "" {
		if _, err := strconv.ParseUint(fraction, 10, 64); err != nil {
			return 0, Invalidf("invalid fractional number")
		}
	}
	for len(fraction) < places {
		fraction += "0"
	}
	frac := int64(0)
	if fraction != "" {
		frac, err = strconv.ParseInt(fraction, 10, 64)
		if err != nil {
			return 0, err
		}
	}
	scale := int64(1)
	for range places {
		scale *= 10
	}
	if whole > (int64(^uint64(0)>>1)-frac)/scale {
		return 0, Invalidf("number is too large")
	}
	return whole*scale + frac, nil
}

func formatDecimalUnits(value int64, places int) string {
	scale := int64(1)
	for range places {
		scale *= 10
	}
	whole, fraction := value/scale, value%scale
	return fmt.Sprintf("%d.%0*d", whole, places, fraction)
}

func ParseTone(value string) (Tone, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" || value == "none" {
		return Tone{Kind: ToneNone}, nil
	}
	if strings.HasPrefix(value, "ctcss:") {
		milliHz, err := parseDecimalUnits(strings.TrimPrefix(value, "ctcss:"), 3)
		if err != nil || milliHz < 50_000 || milliHz > 300_000 {
			return Tone{}, Invalidf("CTCSS tone must be between 50 and 300 Hz with at most 3 decimal places")
		}
		return Tone{Kind: ToneCTCSS, CTCSSMilliHz: &milliHz}, nil
	}
	if strings.HasPrefix(value, "dcs:") {
		parts := strings.Split(strings.TrimPrefix(value, "dcs:"), ":")
		if len(parts) > 2 || parts[0] == "" {
			return Tone{}, Invalidf("DCS tone must be dcs:<code> or dcs:<code>:inverted")
		}
		// DCS codes are octal; keep the digits as written (023 stores as 23).
		_, octErr := strconv.ParseUint(parts[0], 8, 16)
		code, err := strconv.Atoi(parts[0])
		if octErr != nil || err != nil || code < 1 || code > 777 {
			return Tone{}, Invalidf("DCS code must be an octal code between 001 and 777")
		}
		inverted := len(parts) == 2 && parts[1] == "inverted"
		if len(parts) == 2 && !inverted {
			return Tone{}, Invalidf("DCS polarity must be inverted when specified")
		}
		return Tone{Kind: ToneDCS, DCSCode: &code, Inverted: inverted}, nil
	}
	return Tone{}, Invalidf("tone must be none, ctcss:<Hz>, dcs:<code>, or dcs:<code>:inverted")
}

func FormatTone(t Tone) string {
	switch t.Kind {
	case ToneCTCSS:
		if t.CTCSSMilliHz != nil {
			return "ctcss:" + formatDecimalUnits(*t.CTCSSMilliHz, 3)
		}
	case ToneDCS:
		if t.DCSCode != nil {
			suffix := ""
			if t.Inverted {
				suffix = ":inverted"
			}
			return fmt.Sprintf("dcs:%03d%s", *t.DCSCode, suffix)
		}
	}
	return "none"
}

func NormalizeTags(tags []string) []string {
	seen := make(map[string]bool)
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = strings.ToLower(strings.TrimSpace(tag))
		if tag != "" && !seen[tag] {
			seen[tag] = true
			out = append(out, tag)
		}
	}
	return out
}

// NormalizeShortNames trims and collapses whitespace, and drops empty
// entries, duplicates (ignoring case), and copies of the full name.
func NormalizeShortNames(name string, shortNames []string) []string {
	seen := map[string]bool{strings.ToLower(strings.TrimSpace(name)): true}
	out := make([]string, 0, len(shortNames))
	for _, s := range shortNames {
		s = strings.Join(strings.Fields(s), " ")
		if k := strings.ToLower(s); s != "" && !seen[k] {
			seen[k] = true
			out = append(out, s)
		}
	}
	return out
}

// NewLocation builds a location from optional parts, as flags and files
// give them: latitude and longitude together, and a radius only with them.
// All nil is no location.
func NewLocation(lat, lon, radiusKM *float64) (*Location, error) {
	if (lat == nil) != (lon == nil) {
		return nil, Invalidf("latitude and longitude must be given together")
	}
	if lat == nil {
		if radiusKM != nil {
			return nil, Invalidf("a radius needs a latitude and longitude")
		}
		return nil, nil
	}
	loc := &Location{Latitude: *lat, Longitude: *lon, RadiusKM: radiusKM}
	return loc, ValidateLocation(*loc)
}

func ValidateLocation(l Location) error {
	if l.Latitude < -90 || l.Latitude > 90 || l.Longitude < -180 || l.Longitude > 180 {
		return Invalidf("coordinates are outside valid latitude/longitude bounds")
	}
	if l.RadiusKM != nil && *l.RadiusKM <= 0 {
		return Invalidf("radius must be positive")
	}
	return nil
}

func ValidateChannel(c Channel) error {
	if strings.TrimSpace(c.Name) == "" {
		return Invalidf("channel name is required")
	}
	if c.ReceiveHz <= 0 {
		return Invalidf("receive frequency is required")
	}
	if c.TransmitHz != nil && *c.TransmitHz <= 0 {
		return Invalidf("transmit frequency must be positive")
	}
	c.Mode = strings.ToUpper(strings.TrimSpace(c.Mode))
	if !slices.Contains(Modes, c.Mode) {
		return Invalidf("mode must be one of %s", strings.Join(Modes, ", "))
	}
	if _, err := ParseService(string(c.Service)); err != nil {
		return err
	}
	if c.ReceiveTone.Kind == "" || c.TransmitTone.Kind == "" {
		return Invalidf("receive and transmit tones are required; use none when unset")
	}
	if _, err := ParsePowerIntent(string(c.Power)); err != nil {
		return err
	}
	if c.ToneSquelch && c.ReceiveTone.Kind == ToneNone {
		return Invalidf("tone squelch requires a receive tone")
	}
	if _, err := ParseStatus(string(c.Status)); err != nil {
		return err
	}
	for _, n := range c.Networks {
		if _, err := ParseNetworkID(n.String()); err != nil {
			return err
		}
	}
	if c.Location != nil {
		return ValidateLocation(*c.Location)
	}
	return nil
}
