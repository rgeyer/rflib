// Package model describes radio hardware as data: which format programs a
// radio, how many memories it has, what names it can show, its power
// levels, and the frequencies it can tune. Built-in models ship embedded;
// each cites where its facts came from.
package model

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"

	"github.com/ryan/rflib/internal/domain"
	"github.com/ryan/rflib/internal/naming"
	"github.com/ryan/rflib/internal/paths"
	"github.com/ryan/rflib/internal/userfiles"
	"gopkg.in/yaml.v3"
)

// Model is one radio model.
type Model struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	// Format is "chirp" or "profile:<profile name>".
	Format string `yaml:"format"`
	// ChirpModel is what to choose in CHIRP for this radio, for chirp
	// formats.
	ChirpModel string   `yaml:"chirp_model"`
	Memories   Memories `yaml:"memories"`
	Names      Names    `yaml:"names"`
	// Power lists the radio's levels from lowest to highest. Empty means
	// power isn't set per memory.
	Power []Level `yaml:"power"`
	// BandsMHz are the ranges the radio can tune, as [low, high] MHz pairs.
	// Empty means unchecked.
	BandsMHz [][2]float64 `yaml:"bands_mhz"`
	// Modes are the modulation modes a memory can use, from domain.Modes.
	// Empty means unchecked.
	Modes []string `yaml:"modes"`
	// ToneModes are the CHIRP tone modes a memory can use ("" for none,
	// Tone, TSQL, DTCS, Cross), for chirp formats; empty means all.
	// CrossModes are the Cross modes it takes (such as Tone->Tone); empty
	// means all.
	ToneModes  []string `yaml:"tone_modes"`
	CrossModes []string `yaml:"cross_modes"`
	Notes      string   `yaml:"notes"`
	// Source records where the facts came from.
	Source string `yaml:"source"`
}

type Memories struct {
	First int `yaml:"first"`
	Last  int `yaml:"last"`
}

type Names struct {
	MaxLength int `yaml:"max_length"`
	// Charset is a base set: ascii (default), alnum, or upper_numeric.
	Charset string `yaml:"charset"`
	// Extra characters allowed besides the base set, taken literally.
	Extra string `yaml:"extra"`
	// Case "upper" upper-cases names. Sets without lower-case letters are
	// upper-cased regardless.
	Case string `yaml:"case"`
}

// Level is one power setting. Intent says which power intent it serves;
// see Model.Level for how intents without a level are resolved.
type Level struct {
	Label  string             `yaml:"label"`
	Watts  *float64           `yaml:"watts"`
	Intent domain.PowerIntent `yaml:"intent"`
}

// ProfileName returns the profile a profile-format model uses.
func (m Model) ProfileName() (string, bool) {
	return strings.CutPrefix(m.Format, "profile:")
}

func (m Model) Validate() error {
	var errs []error
	fail := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }
	if m.Name == "" {
		fail("name is required")
	}
	if _, isProfile := m.ProfileName(); m.Format != "chirp" && !isProfile {
		fail("format must be chirp or profile:<name>")
	}
	if m.Memories.First < 0 || m.Memories.Last < m.Memories.First {
		fail("memories must run from first to last")
	}
	if m.Names.MaxLength < 0 {
		fail("names.max_length must not be negative")
	}
	if m.Names.Case != "" && m.Names.Case != "upper" {
		fail("names.case must be empty or upper")
	}
	switch m.Names.Charset {
	case "", "ascii", "alnum", "upper_numeric":
	default:
		fail("names.charset must be ascii, alnum, or upper_numeric")
	}
	seen := map[domain.PowerIntent]bool{}
	for i, l := range m.Power {
		if l.Label == "" {
			fail("power level %d needs a label", i+1)
		}
		if l.Intent != domain.PowerUnset {
			if _, err := domain.ParsePowerIntent(string(l.Intent)); err != nil {
				fail("power level %s: %v", l.Label, err)
			} else if seen[l.Intent] {
				fail("power intent %s is on more than one level", l.Intent)
			}
			seen[l.Intent] = true
		}
		if i > 0 && l.Watts != nil && m.Power[i-1].Watts != nil && *l.Watts < *m.Power[i-1].Watts {
			fail("power levels must run from lowest to highest")
		}
	}
	for i, mode := range m.Modes {
		if !slices.Contains(domain.Modes, mode) {
			fail("mode %q must be one of %s", mode, strings.Join(domain.Modes, ", "))
		} else if slices.Contains(m.Modes[:i], mode) {
			fail("mode %s is listed twice", mode)
		}
	}
	for _, b := range m.BandsMHz {
		if b[0] <= 0 || b[1] < b[0] {
			fail("band %v must be [low, high] MHz", b)
		}
	}
	for _, t := range m.ToneModes {
		if !slices.Contains(chirpToneModes, t) {
			fail("tone mode %q must be one of \"\", Tone, TSQL, DTCS, or Cross", t)
		}
	}
	for _, x := range m.CrossModes {
		from, to, ok := strings.Cut(x, "->")
		if !ok || !slices.Contains([]string{"", "Tone", "DTCS"}, from) || !slices.Contains([]string{"", "Tone", "DTCS"}, to) {
			fail("cross mode %q must be like Tone->DTCS", x)
		}
	}
	if len(m.CrossModes) > 0 && len(m.ToneModes) > 0 && !slices.Contains(m.ToneModes, "Cross") {
		fail("cross_modes are for a radio whose tone_modes include Cross")
	}
	return errors.Join(errs...)
}

// chirpToneModes are CHIRP's tone modes.
var chirpToneModes = []string{"", "Tone", "TSQL", "DTCS", "Cross"}

// NameRules are the model's display limits for the naming engine.
func (m Model) NameRules() naming.Rules {
	cs, _ := naming.ParseCharset(m.Names.Charset) // checked by Validate
	if m.Names.Extra != "" {
		cs = cs.WithLiteral(m.Names.Extra)
	}
	return naming.Rules{MaxLen: m.Names.MaxLength, Upper: m.Names.Case == "upper" || !cs.HasLower(), Charset: cs}
}

// Level chooses the level for an intent. A level tagged with the intent
// wins. Otherwise low takes the lowest level, and medium, high, and max
// take the level tagged high, else the highest: a missing middle level
// errs toward being heard. It reports false when the radio has no
// per-memory levels or no intent is given.
func (m Model) Level(p domain.PowerIntent) (Level, bool) {
	if len(m.Power) == 0 || p == domain.PowerUnset {
		return Level{}, false
	}
	for _, l := range m.Power {
		if l.Intent == p {
			return l, true
		}
	}
	if p == domain.PowerLow {
		return m.Power[0], true
	}
	if p != domain.PowerMax {
		for _, l := range m.Power {
			if l.Intent == domain.PowerHigh {
				return l, true
			}
		}
	}
	return m.Power[len(m.Power)-1], true
}

// Capped returns the highest level at or under capWatts, and whether one
// exists. Levels without known watts can't be checked and are skipped.
func (m Model) Capped(capWatts float64) (Level, bool) {
	var best Level
	found := false
	for _, l := range m.Power {
		if l.Watts != nil && *l.Watts <= capWatts {
			best, found = l, true
		}
	}
	return best, found
}

// Tunes reports whether hz is inside the model's bands, or true when the
// model lists none.
func (m Model) Tunes(hz int64) bool {
	if len(m.BandsMHz) == 0 {
		return true
	}
	mhz := float64(hz) / 1e6
	return slices.ContainsFunc(m.BandsMHz, func(b [2]float64) bool { return mhz >= b[0] && mhz <= b[1] })
}

// HasMode reports whether a memory can use mode, or true when the model
// lists no modes.
func (m Model) HasMode(mode string) bool {
	return len(m.Modes) == 0 || slices.Contains(m.Modes, mode)
}

// Receives reports whether the radio can receive a channel: its receive
// frequency is in the bands and its mode is one the radio has.
func (m Model) Receives(c domain.Channel) bool {
	return m.Tunes(c.ReceiveHz) && m.HasMode(c.Mode)
}

func Parse(data []byte) (Model, error) {
	var m Model
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&m); err != nil {
		return m, fmt.Errorf("read model: %w", err)
	}
	if err := m.Validate(); err != nil {
		return m, fmt.Errorf("model %s: %w", m.Name, err)
	}
	return m, nil
}

//go:embed builtin/*.yaml
var builtinFS embed.FS

// UserDir is where user models live: models in rflib's configuration
// directory (see paths.ConfigDir).
func UserDir() (string, error) {
	dir, err := paths.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "models"), nil
}

// Entry is a model available by name.
type Entry struct {
	Model Model
	// Origin is "built-in" or the file the model was loaded from.
	Origin string
	// Builtin is the built-in model a user file replaces, by having its
	// name; nil when it replaces none.
	Builtin *Model
	data    []byte
}

// Local reports whether the model comes from a file of the user's own.
func (e Entry) Local() bool { return e.Origin != builtinOrigin }

// SameAsBuiltin reports whether a user file replacing a built-in model now
// says just what the built-in does, such as once a fix sent as a file is
// in a release. Removing the file then changes nothing, and lets later
// fixes to the built-in through.
func (e Entry) SameAsBuiltin() bool {
	return e.Builtin != nil && reflect.DeepEqual(e.Model, *e.Builtin)
}

// Status says where a model comes from: "built-in", "local", "replaces
// the built-in", or "same as the built-in".
func (e Entry) Status() string {
	switch {
	case !e.Local():
		return builtinOrigin
	case e.SameAsBuiltin():
		return "same as the built-in"
	case e.Builtin != nil:
		return "replaces the built-in"
	}
	return "local"
}

const builtinOrigin = "built-in"

// files are the user's model files.
var files = userfiles.Kind[Model]{What: "model", Dir: UserDir, Parse: Parse, Name: func(m Model) string { return m.Name }}

// Problems lists the user's model files that can't be used, which
// Available leaves out.
func Problems() []userfiles.Problem {
	_, problems := files.Scan()
	return problems
}

// Install checks a model file and copies it into UserDir, replacing an
// installed model of the same name only with replace. It returns the
// installed model and the file it replaced ("" for none).
func Install(path string, replace bool) (Entry, string, error) {
	f, old, err := files.Install(path, replace)
	if err != nil {
		return Entry{}, "", err
	}
	e, err := Lookup(f.Value.Name)
	return e, old, err
}

// Remove deletes the user's file for a model, and returns it as it was.
func Remove(name string) (Entry, error) {
	es, _ := Available()
	var was Entry
	for _, e := range es {
		if e.Model.Name == name {
			was = e
		}
	}
	if was.Model.Name != "" && !was.Local() {
		return was, domain.Invalidf("%s is a built-in model, with no file of yours to remove", name)
	}
	f, err := files.Remove(name)
	if err != nil {
		return was, domain.Invalidf("%v", err)
	}
	if was.Model.Name == "" {
		was.Origin = f.Path
	}
	return was, nil
}

// Builtins returns the built-in models by name.
func Builtins() ([]Model, error) {
	es, err := builtins()
	if err != nil {
		return nil, err
	}
	return models(es), nil
}

func builtins() ([]Entry, error) {
	paths, err := fs.Glob(builtinFS, "builtin/*.yaml")
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(paths))
	for _, p := range paths {
		data, err := builtinFS.ReadFile(p)
		if err != nil {
			return nil, err
		}
		m, err := Parse(data)
		if err != nil {
			return nil, fmt.Errorf("built-in %s: %w", p, err)
		}
		out = append(out, Entry{Model: m, Origin: builtinOrigin, data: data})
	}
	return out, nil
}

// Available lists the built-in models and those in UserDir, by name. A user
// model with a built-in's name replaces it, so a model can be fixed locally.
// User files that can't be used are left out (see Problems).
func Available() ([]Entry, error) {
	es, err := builtins()
	if err != nil {
		return nil, err
	}
	byName := map[string]Entry{}
	for _, e := range es {
		byName[e.Model.Name] = e
	}
	user, _ := files.Scan()
	for _, f := range user {
		e := Entry{Model: f.Value, Origin: f.Path, data: f.Data}
		if b, ok := byName[f.Value.Name]; ok && !b.Local() {
			e.Builtin = &b.Model
		}
		byName[f.Value.Name] = e
	}
	out := make([]Entry, 0, len(byName))
	for _, e := range byName {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Model.Name < out[j].Model.Name })
	return out, nil
}

// Load reads and validates a model file.
func Load(path string) (Model, error) {
	e, err := load(path)
	return e.Model, err
}

func load(path string) (Entry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Entry{}, err
	}
	m, err := Parse(data)
	if err != nil {
		return Entry{}, fmt.Errorf("%s: %w", path, err)
	}
	return Entry{Model: m, Origin: path, data: data}, nil
}

// All returns the available models (built-in and user) by name.
func All() ([]Model, error) {
	es, err := Available()
	if err != nil {
		return nil, err
	}
	return models(es), nil
}

func models(es []Entry) []Model {
	out := make([]Model, len(es))
	for i, e := range es {
		out[i] = e.Model
	}
	return out
}

// Lookup returns an available model's entry by name.
func Lookup(name string) (Entry, error) {
	es, err := Available()
	if err != nil {
		return Entry{}, err
	}
	names := make([]string, len(es))
	for i, e := range es {
		if e.Model.Name == name {
			return e, nil
		}
		names[i] = e.Model.Name
	}
	if p, ok := userfiles.ProblemWith(Problems(), name); ok {
		return Entry{}, domain.Invalidf("the model file %s can't be used: %v", p.Path, p.Err)
	}
	return Entry{}, domain.Invalidf("unknown radio model %q; available: %s", name, strings.Join(names, ", "))
}

// Find returns an available model by name.
func Find(name string) (Model, error) {
	e, err := Lookup(name)
	return e.Model, err
}

// YAML returns a model's source, for `model show` and `model new`.
func (e Entry) YAML() []byte { return e.data }
