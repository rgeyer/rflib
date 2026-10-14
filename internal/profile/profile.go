// Package profile reads and writes CSV formats described by data rather than
// code: which columns a programming tool expects, which library field fills
// each one, and how library values are spelled in that tool (for example,
// tone squelch as "TONE SQL"). The same profile drives export and import.
package profile

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/ryan/rflib/internal/domain"
	"github.com/ryan/rflib/internal/naming"
	"gopkg.in/yaml.v3"
)

// Profile describes one CSV memory format.
type Profile struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	// Header writes column names as the first row and expects one on import.
	Header bool `yaml:"header"`
	// LineEnding is "lf" (default) or "crlf".
	LineEnding string `yaml:"line_ending"`
	// Start is the first memory location number (default 1).
	Start *int `yaml:"start"`
	// Slots pads the output with empty memories up to this many rows, as
	// tools that export a whole radio do. 0 writes only the channels, and
	// otherwise more channels than slots fails the export.
	Slots int `yaml:"slots"`
	// MaxNameLength truncates names; 0 keeps them whole.
	MaxNameLength int `yaml:"max_name_length"`
	// NameCase is "" (unchanged) or "upper".
	NameCase string `yaml:"name_case"`
	// NameCharset limits the characters in names: "ascii" (default),
	// "alnum", or a literal set such as "A-Z0-9 -/" (see naming.ParseCharset).
	NameCharset string `yaml:"name_charset"`
	// StandardTonesOnly rejects CTCSS/DCS tones outside the standard sets.
	StandardTonesOnly bool     `yaml:"standard_tones_only"`
	Columns           []Column `yaml:"columns"`
	// Notes is shown by `profile show`, for caveats about the format.
	Notes string `yaml:"notes"`
}

// Column is one CSV column. Source names the library value that fills it
// (see Sources); the value is formatted, then translated through Map.
type Column struct {
	Name   string `yaml:"name"`
	Source string `yaml:"source"`
	// Value is the fixed text of a "const" column.
	Value string `yaml:"value"`
	// Decimals sets the digits after the point for frequencies and tones.
	Decimals *int `yaml:"decimals"`
	// Suffix is appended to formatted numbers ("100.0" becomes "100.0 Hz")
	// and stripped on import.
	Suffix string `yaml:"suffix"`
	// Map translates library values to this tool's text. The key "*"
	// catches any value not listed. With a map, an unlisted value is an
	// error rather than passed through.
	Map map[string]string `yaml:"map"`
	// Default is written when the source has no value, such as the CTCSS
	// column of a channel without a tone.
	Default string `yaml:"default"`
	// Empty is written in padding rows (see Profile.Slots).
	Empty string `yaml:"empty"`
	// StepsKHz lists the steps a "step_khz" column may choose from, in order
	// of preference; the first that evenly divides the receive frequency
	// wins.
	StepsKHz []string `yaml:"steps_khz"`
}

// Sources are the library values a column can draw from.
var Sources = map[string]string{
	"location":    "memory location number",
	"name":        "channel name, truncated to max_name_length",
	"rx_mhz":      "receive frequency in MHz",
	"tx_mhz":      "transmit frequency in MHz (receive frequency when the channel has none or is receive only)",
	"offset_mhz":  "repeater offset in MHz; 0 for simplex, split, and receive only",
	"duplex":      "none, plus, minus, split, or off (receive only)",
	"mode":        "FM, NFM, AM, USB, or LSB",
	"narrow":      "true for NFM, false otherwise",
	"tone_mode":   "none, tone, tsql, dcs, dcs_enc, tone_dcs, dcs_tone, tone_tone, dcs_dcs, rx_tone, or rx_dcs",
	"ctcss_hz":    "the channel's CTCSS tone in Hz (transmit tone if any, else receive)",
	"tx_ctcss_hz": "transmit CTCSS tone in Hz",
	"rx_ctcss_hz": "receive (squelch) CTCSS tone in Hz",
	"dcs_code":    "the channel's DCS code (transmit code if any, else receive)",
	"tx_dcs_code": "transmit DCS code",
	"rx_dcs_code": "receive (squelch) DCS code",
	"dcs_polarity": "transmit then receive DCS polarity: NN, NR, RN, or RR. Without this column, " +
		"inverted DCS can't be exported",
	"power":    "the radio's power level label when exporting a radio, else the channel's power intent (LOW, MEDIUM, HIGH, MAX); upper-cased",
	"skip":     "true when the channel is skipped while scanning",
	"notes":    "notes on one line",
	"service":  "radio service",
	"tags":     "tags joined with ;",
	"step_khz": "tuning step in kHz, chosen from steps_khz",
	"const":    "the column's fixed value",
}

// Validate checks the profile definition itself.
func (p Profile) Validate() error {
	var errs []error
	fail := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }
	if p.Name == "" {
		fail("name is required")
	}
	if p.LineEnding != "" && p.LineEnding != "lf" && p.LineEnding != "crlf" {
		fail("line_ending must be lf or crlf")
	}
	if p.NameCase != "" && p.NameCase != "upper" {
		fail("name_case must be empty or upper")
	}
	if _, err := naming.ParseCharset(p.NameCharset); err != nil {
		fail("name_charset: %v", err)
	}
	if p.Slots < 0 || p.MaxNameLength < 0 || p.Start != nil && *p.Start < 0 {
		fail("start, slots, and max_name_length must not be negative")
	}
	hasRX := false
	for i, c := range p.Columns {
		where := fmt.Sprintf("column %d (%s)", i+1, c.Name)
		if _, ok := Sources[c.Source]; !ok {
			fail("%s: unknown source %q", where, c.Source)
		}
		if c.Source == "rx_mhz" {
			hasRX = true
		}
		if c.Decimals != nil && (*c.Decimals < 0 || *c.Decimals > 6) {
			fail("%s: decimals must be 0 to 6", where)
		}
		if c.Source == "step_khz" && len(c.StepsKHz) == 0 {
			fail("%s: step_khz needs steps_khz", where)
		}
		for _, s := range c.StepsKHz {
			if _, err := parseKHz(s); err != nil {
				fail("%s: steps_khz %q: %v", where, s, err)
			}
		}
	}
	if !hasRX {
		fail("a column with source rx_mhz is required")
	}
	return errors.Join(errs...)
}

// NameRules are the profile's display limits for the naming engine.
func (p Profile) NameRules() naming.Rules {
	cs, _ := naming.ParseCharset(p.NameCharset) // checked by Validate
	return naming.Rules{MaxLen: p.MaxNameLength, Upper: p.NameCase == "upper", Charset: cs}
}

func (p Profile) start() int {
	if p.Start == nil {
		return 1
	}
	return *p.Start
}

// Parse reads a YAML profile and validates it.
func Parse(data []byte) (Profile, error) {
	var p Profile
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&p); err != nil {
		return p, fmt.Errorf("read profile: %w", err)
	}
	if err := p.Validate(); err != nil {
		return p, fmt.Errorf("profile %s: %w", p.Name, err)
	}
	return p, nil
}

//go:embed builtin/*.yaml
var builtinFS embed.FS

// UserDir is where user profiles live: $XDG_CONFIG_HOME/rflib/profiles (or
// the OS equivalent).
func UserDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "rflib", "profiles"), nil
}

// Entry is a profile available by name.
type Entry struct {
	Profile Profile
	// Origin is "built-in" or the file the profile was loaded from.
	Origin string
}

// Available lists built-in profiles and those in UserDir. A user profile
// with a built-in's name replaces it, so a format can be fixed locally.
func Available() ([]Entry, error) {
	byName := map[string]Entry{}
	builtins, err := fs.Glob(builtinFS, "builtin/*.yaml")
	if err != nil {
		return nil, err
	}
	for _, path := range builtins {
		data, err := builtinFS.ReadFile(path)
		if err != nil {
			return nil, err
		}
		p, err := Parse(data)
		if err != nil {
			return nil, fmt.Errorf("built-in %s: %w", path, err)
		}
		byName[p.Name] = Entry{p, "built-in"}
	}
	if dir, err := UserDir(); err == nil {
		files, _ := filepath.Glob(filepath.Join(dir, "*.yaml"))
		more, _ := filepath.Glob(filepath.Join(dir, "*.yml"))
		for _, path := range append(files, more...) {
			p, err := Load(path)
			if err != nil {
				return nil, err
			}
			byName[p.Name] = Entry{p, path}
		}
	}
	out := make([]Entry, 0, len(byName))
	for _, e := range byName {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Profile.Name < out[j].Profile.Name })
	return out, nil
}

// Load reads a profile file.
func Load(path string) (Profile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Profile{}, err
	}
	p, err := Parse(data)
	if err != nil {
		return p, fmt.Errorf("%s: %w", path, err)
	}
	return p, nil
}

// Find resolves a profile reference: a path when it ends in .yaml or .yml
// or contains a path separator, otherwise a name from Available.
func Find(ref string) (Entry, error) {
	if strings.HasSuffix(ref, ".yaml") || strings.HasSuffix(ref, ".yml") || strings.ContainsRune(ref, os.PathSeparator) {
		p, err := Load(ref)
		return Entry{p, ref}, err
	}
	entries, err := Available()
	if err != nil {
		return Entry{}, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.Profile.Name == ref {
			return e, nil
		}
		names = append(names, e.Profile.Name)
	}
	slices.Sort(names)
	return Entry{}, domain.Invalidf("unknown profile %q; available: %s", ref, strings.Join(names, ", "))
}

// BuiltinYAML returns a built-in profile's source, for `profile show`.
func BuiltinYAML(name string) ([]byte, error) {
	return builtinFS.ReadFile("builtin/" + name + ".yaml")
}
