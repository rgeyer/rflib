package profile

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ryan/rflib/internal/domain"
)

var update = flag.Bool("update", false, "rewrite golden files")

func builtin(t *testing.T, name string) Profile {
	t.Helper()
	e, err := Find(name)
	if err != nil {
		t.Fatal(err)
	}
	return e.Profile
}

func tone(t *testing.T, v string) domain.Tone {
	t.Helper()
	x, err := domain.ParseTone(v)
	if err != nil {
		t.Fatal(err)
	}
	return x
}

func channels(t *testing.T, p Profile, data string) []domain.Channel {
	t.Helper()
	b, err := Parser{Profile: p}.Parse(strings.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Errors) > 0 {
		t.Fatalf("parse errors: %v", b.Errors)
	}
	out := make([]domain.Channel, len(b.Records))
	for i, r := range b.Records {
		out[i] = r.Channel
	}
	return out
}

func TestBuiltinsAreValid(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // no user profiles
	entries, err := Available()
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Profile.Name)
		if e.Origin != "built-in" {
			t.Errorf("%s origin = %s", e.Profile.Name, e.Origin)
		}
	}
	if strings.Join(names, ",") != "yaesu-ftm150,yaesu-ftm6000" {
		t.Fatalf("built-ins = %v", names)
	}
}

func TestParseFTM150Sample(t *testing.T) {
	data, err := os.ReadFile("testdata/ftm150-sample.csv")
	if err != nil {
		t.Fatal(err)
	}
	cs := channels(t, builtin(t, "yaesu-ftm150"), string(data))
	if len(cs) != 7 {
		t.Fatalf("got %d channels, want 7 (empty memories ignored)", len(cs))
	}
	byName := map[string]domain.Channel{}
	for _, c := range cs {
		byName[c.Name] = c
	}
	check := func(name, want string, got string) {
		t.Helper()
		if got != want {
			t.Errorf("%s: got %q, want %q", name, got, want)
		}
	}
	metro := byName["Metro"]
	check("Metro tx", "146.340000", domain.FormatFrequencyMHz(*metro.TransmitHz))
	check("Metro tone", "ctcss:127.300", domain.FormatTone(metro.TransmitTone))
	check("Metro squelch", "none", domain.FormatTone(metro.ReceiveTone))
	check("Metro power", "MID", metro.Power)
	if !metro.Skip || byName["Calling"].Skip {
		t.Error("Scan NO should mean skip")
	}
	check("Calling notes", "National simplex", byName["Calling"].Notes)
	gmrs := byName["GMRS 15R"]
	check("GMRS tx", "467.550000", domain.FormatFrequencyMHz(*gmrs.TransmitHz))
	check("GMRS DCS tx", "dcs:413", domain.FormatTone(gmrs.TransmitTone))
	check("GMRS DCS rx", "dcs:413", domain.FormatTone(gmrs.ReceiveTone))
	if !gmrs.ToneSquelch {
		t.Error("DCS should squelch")
	}
	check("narrow", "NFM", byName["GMRS 1"].Mode)
	check("unused tone ignored", "none", domain.FormatTone(byName["GMRS 1"].TransmitTone))
	check("AM", "AM", byName["Tower"].Mode)
	check("unnamed", "145.120000", byName["145.120000"].Name)
}

func TestExportFTM150Golden(t *testing.T) {
	p := builtin(t, "yaesu-ftm150")
	data, err := os.ReadFile("testdata/ftm150-sample.csv")
	if err != nil {
		t.Fatal(err)
	}
	cs := channels(t, p, string(data))
	var out bytes.Buffer
	if err := (Exporter{Profile: p}).Export(&out, cs); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join("testdata", "ftm150-export.golden.csv")
	if *update {
		if err := os.WriteFile(path, out.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test ./internal/profile -update)", err)
	}
	if !bytes.Equal(out.Bytes(), want) {
		t.Fatalf("export differs from %s:\n%s", path, out.String())
	}
	if lines := strings.Count(out.String(), "\n"); lines != 999 {
		t.Fatalf("export has %d rows, want 999 memory slots", lines)
	}

	// The export re-imports to the same channels.
	again := channels(t, p, out.String())
	if len(again) != len(cs) {
		t.Fatalf("re-import has %d channels, want %d", len(again), len(cs))
	}
	for i := range cs {
		a, b := cs[i], again[i]
		if a.Name != b.Name || a.ReceiveHz != b.ReceiveHz || *a.TransmitHz != *b.TransmitHz || a.Mode != b.Mode ||
			domain.FormatTone(a.TransmitTone) != domain.FormatTone(b.TransmitTone) || domain.FormatTone(a.ReceiveTone) != domain.FormatTone(b.ReceiveTone) ||
			a.Skip != b.Skip || a.Power != b.Power || a.Notes != b.Notes {
			t.Errorf("channel %d changed on round trip:\n%+v\n%+v", i, a, b)
		}
	}
}

func TestExportRejectsUnsupportedChannels(t *testing.T) {
	p := builtin(t, "yaesu-ftm150")
	none := tone(t, "none")
	base := domain.Channel{Name: "x", ReceiveHz: 146520000, Mode: "FM", TransmitTone: none, ReceiveTone: none}
	for _, tc := range []struct {
		name string
		edit func(*domain.Channel)
		want string
	}{
		{"tone squelch", func(c *domain.Channel) {
			c.TransmitTone, c.ReceiveTone, c.ToneSquelch = tone(t, "ctcss:100"), tone(t, "ctcss:100"), true
		}, "tone squelch (tone mode \"tsql\")"},
		{"usb", func(c *domain.Channel) { c.Mode = "USB" }, "mode USB"},
		{"inverted dcs", func(c *domain.Channel) { c.TransmitTone = tone(t, "dcs:023:inverted") }, "inverted DCS"},
		{"odd ctcss", func(c *domain.Channel) { c.TransmitTone = tone(t, "ctcss:100.5") }, "standard"},
		{"6 decimals", func(c *domain.Channel) { c.ReceiveHz = 146520001 }, "more than 5 decimals"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := base
			tc.edit(&c)
			err := Exporter{Profile: p}.Validate(c)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}

	p.Slots = 1
	var out bytes.Buffer
	if err := (Exporter{Profile: p}).Export(&out, []domain.Channel{base, base}); err == nil || !strings.Contains(err.Error(), "holds 1 memories") {
		t.Fatalf("slots: err = %v", err)
	}
}

// A user profile can rename tone modes and change the file layout without
// code changes.
func TestCustomProfile(t *testing.T) {
	p, err := Parse([]byte(`
name: custom
header: true
line_ending: crlf
start: 0
max_name_length: 4
name_case: upper
columns:
  - {name: Ch, source: location}
  - {name: Name, source: name}
  - {name: RX, source: rx_mhz, decimals: 4}
  - {name: Shift, source: duplex, map: {none: S, plus: "+", minus: "-", "off": RX}}
  - {name: Squelch, source: tone_mode, map: {none: "", tone: T, tsql: T-SQL, tone_dcs: "T>D"}}
  - {name: TX tone, source: tx_ctcss_hz, decimals: 2}
  - {name: RX code, source: rx_dcs_code}
  - {name: Svc, source: service}
`))
	if err != nil {
		t.Fatal(err)
	}
	tx := int64(146_340_000)
	cs := []domain.Channel{
		{Name: "metro rpt", ReceiveHz: 146_940_000, TransmitHz: &tx, Mode: "FM", Service: domain.ServiceAmateur, TransmitTone: tone(t, "ctcss:100"), ReceiveTone: tone(t, "ctcss:100"), ToneSquelch: true},
		{Name: "mixed", ReceiveHz: 146_520_000, TransmitHz: &[]int64{146_520_000}[0], Mode: "FM", TransmitTone: tone(t, "ctcss:88.5"), ReceiveTone: tone(t, "dcs:754"), ToneSquelch: true},
		{Name: "wx", ReceiveHz: 162_550_000, Mode: "FM", RXOnly: true, TransmitTone: tone(t, "none"), ReceiveTone: tone(t, "none")},
	}
	var out bytes.Buffer
	if err := (Exporter{Profile: p}).Export(&out, cs); err != nil {
		t.Fatal(err)
	}
	want := "Ch,Name,RX,Shift,Squelch,TX tone,RX code,Svc\r\n" +
		"0,METR,146.9400,-,T-SQL,100.00,,amateur\r\n" +
		"1,MIXE,146.5200,S,T>D,88.50,754,\r\n" +
		"2,WX,162.5500,RX,,,,\r\n"
	if out.String() != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out.String(), want)
	}
	back := channels(t, p, out.String())
	if len(back) != 3 || domain.FormatTone(back[1].ReceiveTone) != "dcs:754" || back[2].TransmitHz != nil || back[0].Service != domain.ServiceAmateur {
		t.Fatalf("re-import = %+v", back)
	}
}

func TestUserProfileOverridesBuiltin(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	dir := filepath.Join(cfg, "rflib", "profiles")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	src, err := BuiltinYAML("yaesu-ftm150")
	if err != nil {
		t.Fatal(err)
	}
	fixed := strings.Replace(string(src), "map: {none: \"OFF\", tone: TONE, dcs: DCS}", "map: {none: \"OFF\", tone: TONE, tsql: TONE SQL, dcs: DCS}", 1)
	if err := os.WriteFile(filepath.Join(dir, "ftm150.yaml"), []byte(fixed), 0o644); err != nil {
		t.Fatal(err)
	}
	e, err := Find("yaesu-ftm150")
	if err != nil {
		t.Fatal(err)
	}
	if e.Origin == "built-in" || e.Profile.Columns[7].Map["tsql"] != "TONE SQL" {
		t.Fatalf("user profile not used: %s", e.Origin)
	}
}

func TestProfileValidation(t *testing.T) {
	for _, tc := range []struct{ yaml, want string }{
		{"name: x\ncolumns: [{name: A, source: name}]", "rx_mhz is required"},
		{"name: x\ncolumns: [{name: A, source: rx_mhz}, {name: B, source: freq}]", `unknown source "freq"`},
		{"name: x\ncolumns: [{name: A, source: rx_mhz, decimals: 7}]", "decimals must be 0 to 6"},
		{"name: x\ncolumns: [{name: A, source: rx_mhz}, {name: S, source: step_khz}]", "needs steps_khz"},
		{"name: x\nline_ending: cr\ncolumns: [{name: A, source: rx_mhz}]", "line_ending"},
		{"name: x\ncolumn: []", "field column not found"},
	} {
		if _, err := Parse([]byte(tc.yaml)); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("Parse(%q): err = %v, want %q", tc.yaml, err, tc.want)
		}
	}
}

func TestStepChoice(t *testing.T) {
	col := Column{Name: "Step", Source: "step_khz", StepsKHz: []string{"25", "5", "6.25", "12.5"}}
	for hz, want := range map[int64]string{146_520_000: "5", 446_000_000: "25", 462_562_500: "6.25", 151_820_000: "5"} {
		got, err := row{c: domain.Channel{ReceiveHz: hz}}.raw(Profile{}, col)
		if err != nil || got != want {
			t.Errorf("%d Hz: got %q, %v; want %q", hz, got, err, want)
		}
	}
	if _, err := (row{c: domain.Channel{ReceiveHz: 146_521_000}}).raw(Profile{}, col); err == nil {
		t.Error("expected no step to fit 146.521 MHz")
	}
}
