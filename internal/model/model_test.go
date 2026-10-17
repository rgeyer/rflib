package model

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ryan/rflib/internal/domain"
	"github.com/ryan/rflib/internal/naming"
	"github.com/ryan/rflib/internal/profile"
	"gopkg.in/yaml.v3"
)

func TestBuiltinsAreValid(t *testing.T) {
	all, err := Builtins()
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, m := range all {
		names = append(names, m.Name)
		if m.Source == "" {
			t.Errorf("%s doesn't cite a source", m.Name)
		}
		if p, ok := m.ProfileName(); ok {
			if _, err := profile.Find(p); err != nil {
				t.Errorf("%s: %v", m.Name, err)
			}
		}
	}
	want := "baofeng-uv5rm,chirp-generic,explorer-qrz1,radioddity-uv5g,tidradio-tdh3-gmrs,tyt-th9800,yaesu-ft857,yaesu-ftm150,yaesu-ftm6000"
	if got := strings.Join(names, ","); got != want {
		t.Fatalf("built-ins = %s", got)
	}
}

func mustFind(t *testing.T, name string) Model {
	t.Helper()
	m, err := Find(name)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestLevel(t *testing.T) {
	th := mustFind(t, "tyt-th9800")        // Low, Mid2, Mid1 (medium), High
	uv5g := mustFind(t, "radioddity-uv5g") // Low, High
	ft857 := mustFind(t, "yaesu-ft857")    // no levels
	for _, tc := range []struct {
		m    Model
		p    domain.PowerIntent
		want string
	}{
		{th, domain.PowerLow, "Low"}, {th, domain.PowerMedium, "Mid1"}, {th, domain.PowerHigh, "High"}, {th, domain.PowerMax, "High"},
		{uv5g, domain.PowerMedium, "High"}, // no middle level: err toward being heard
		{uv5g, domain.PowerMax, "High"},
	} {
		l, ok := tc.m.Level(tc.p)
		if !ok || l.Label != tc.want {
			t.Errorf("%s %s = %q, %v; want %s", tc.m.Name, tc.p, l.Label, ok, tc.want)
		}
	}
	if _, ok := ft857.Level(domain.PowerHigh); ok {
		t.Error("FT-857 has no per-memory power")
	}
	if _, ok := th.Level(domain.PowerUnset); ok {
		t.Error("unset intent picked a level")
	}
	if l, ok := uv5g.Capped(2); !ok || l.Label != "Low" {
		t.Errorf("UV-5G capped at 2 W = %+v, %v", l, ok)
	}
	if _, ok := uv5g.Capped(0.5); ok {
		t.Error("UV-5G has no level at 0.5 W")
	}
}

func TestNameRules(t *testing.T) {
	th := mustFind(t, "tyt-th9800").NameRules()
	if got := naming.Fit("Metro rpt #2", nil, th).Name; len(got) > 6 || got != strings.ToUpper(got) {
		t.Errorf("TH-9800 name = %q", got)
	}
	if !th.Upper {
		t.Error("a set without lower case must upper-case names")
	}
	// Literal extras aren't ranges: "+-=" allows those three only.
	td := mustFind(t, "tidradio-tdh3-gmrs").NameRules()
	if !td.Charset.Allows('-') || td.Charset.Allows(',') == false || td.Charset.Allows('~') || td.Charset.Allows('{') {
		t.Error("TD-H3 charset parsed as ranges")
	}
	if td.Upper {
		t.Error("TD-H3 allows lower case")
	}
}

func TestTunes(t *testing.T) {
	q := mustFind(t, "explorer-qrz1")
	if !q.Tunes(146_520_000) || q.Tunes(162_550_000+400_000_000) || q.Tunes(27_185_000) {
		t.Error("QRZ-1 bands")
	}
	if ftm := mustFind(t, "yaesu-ftm150"); !ftm.Tunes(162_550_000) || !ftm.Tunes(118_000_000) || ftm.Tunes(27_185_000) || ftm.Tunes(902_000_000) {
		t.Error("FTM-150 bands")
	}
	if !(Model{}).Tunes(27_000_000) {
		t.Error("models without bands accept anything")
	}
}

func TestReceives(t *testing.T) {
	ssb := domain.Channel{Name: "2m SSB Call", ReceiveHz: 144_200_000, Mode: "USB"}
	fm := domain.Channel{Name: "2m FM Call", ReceiveHz: 146_520_000, Mode: "FM"}
	if uv := mustFind(t, "baofeng-uv5rm"); uv.Receives(ssb) || !uv.Receives(fm) || !uv.HasMode("AM") {
		t.Error("UV-5RM modes")
	}
	if ft := mustFind(t, "yaesu-ft857"); !ft.Receives(ssb) {
		t.Error("FT-857 takes USB")
	}
	if !(Model{}).HasMode("LSB") {
		t.Error("models without modes accept any")
	}
}

func TestValidate(t *testing.T) {
	for _, tc := range []struct{ yaml, want string }{
		{"name: x\nformat: kenwood\nmemories: {first: 1, last: 2}", "format must be"},
		{"name: x\nformat: chirp\nmemories: {first: 5, last: 2}", "memories must run"},
		{"name: x\nformat: chirp\nmemories: {first: 1, last: 2}\npower: [{label: H, watts: 5}, {label: L, watts: 1}]", "lowest to highest"},
		{"name: x\nformat: chirp\nmemories: {first: 1, last: 2}\npower: [{label: A, intent: low}, {label: B, intent: low}]", "more than one level"},
		{"name: x\nformat: chirp\nmemories: {first: 1, last: 2}\nnames: {charset: klingon}", "names.charset"},
		{"name: x\nformat: chirp\nmemories: {first: 1, last: 2}\nmodes: [FM, DMR]", `mode "DMR" must be one of`},
		{"name: x\nformat: chirp\nmemories: {first: 1, last: 2}\nmodes: [FM, FM]", "listed twice"},
	} {
		if _, err := Parse([]byte(tc.yaml)); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("err = %v, want %q", err, tc.want)
		}
	}
}

func TestUserModels(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	dir := filepath.Join(cfg, "rflib", "models")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, yaml string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(yaml), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("mine.yaml", "name: my-ht\nformat: chirp\nmemories: {first: 0, last: 127}\nnames: {max_length: 6}\n")
	write("fix.yml", "name: yaesu-ft857\nformat: chirp\nmemories: {first: 1, last: 10}\n")
	e, err := Lookup("my-ht")
	if err != nil || e.Origin != filepath.Join(dir, "mine.yaml") || e.Model.Names.MaxLength != 6 || !strings.HasPrefix(string(e.YAML()), "name: my-ht") {
		t.Fatalf("user model = %+v, %v", e, err)
	}
	if m := mustFind(t, "yaesu-ft857"); m.Memories.Last != 10 {
		t.Fatalf("user model didn't replace the built-in: %+v", m)
	}
	if b, _ := Builtins(); slices.ContainsFunc(b, func(m Model) bool { return m.Name == "my-ht" }) {
		t.Fatal("Builtins includes a user model")
	}
	if e, err := Lookup("yaesu-ft857"); err != nil || e.Status() != "replaces the built-in" || e.Builtin == nil || e.Builtin.Memories.Last != 200 {
		t.Fatalf("replacement = %+v %v", e, err)
	}
	if e, _ := Lookup("my-ht"); e.Status() != "local" {
		t.Errorf("my-ht = %s", e.Status())
	}

	// A broken file is skipped, and everything else still works; looking
	// it up by its file's name says what's wrong with it.
	write("half-done.yaml", "name: half-done\nformat: nope\n")
	if _, err := All(); err != nil {
		t.Fatalf("a broken user model broke the rest: %v", err)
	}
	if mustFind(t, "my-ht").Names.MaxLength != 6 || mustFind(t, "chirp-generic").Name != "chirp-generic" {
		t.Error("models went missing")
	}
	if ps := Problems(); len(ps) != 1 || filepath.Base(ps[0].Path) != "half-done.yaml" {
		t.Errorf("problems = %v", ps)
	}
	if _, err := Find("half-done"); err == nil || !strings.Contains(err.Error(), "half-done.yaml can't be used") {
		t.Errorf("finding the broken model = %v", err)
	}

	// A replacement that matches the built-in says so.
	b, err := Lookup("yaesu-ft857")
	if err != nil {
		t.Fatal(err)
	}
	write("fix.yml", string(b.Builtin.mustYAML(t)))
	if e, _ := Lookup("yaesu-ft857"); !e.SameAsBuiltin() || e.Status() != "same as the built-in" {
		t.Errorf("same as built-in = %s", e.Status())
	}
	if _, err := Remove("yaesu-ft857"); err != nil {
		t.Fatal(err)
	}
	if e, _ := Lookup("yaesu-ft857"); e.Local() {
		t.Errorf("after remove, yaesu-ft857 is from %s", e.Origin)
	}
	if _, err := Remove("chirp-generic"); err == nil || !strings.Contains(err.Error(), "built-in") {
		t.Errorf("removing a built-in = %v", err)
	}
}

// mustYAML is the model as a file, for tests.
func (m Model) mustYAML(t *testing.T) []byte {
	t.Helper()
	b, err := yaml.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
