package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type cli struct {
	t  *testing.T
	db string
}

func newCLI(t *testing.T) *cli {
	t.Helper()
	c := &cli{t: t, db: filepath.Join(t.TempDir(), "lib.db")}
	c.ok("init")
	return c
}

// exec runs rflib with --db prepended and returns stdout, stderr, and error.
func (c *cli) exec(args ...string) (string, string, error) {
	var out, errOut bytes.Buffer
	err := run(append([]string{"--db", c.db}, args...), &out, &errOut)
	return out.String(), errOut.String(), err
}

func (c *cli) ok(args ...string) string {
	c.t.Helper()
	out, _, err := c.exec(args...)
	if err != nil {
		c.t.Fatalf("rflib %s: %v", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(out)
}

func (c *cli) fails(want string, args ...string) {
	c.t.Helper()
	_, _, err := c.exec(args...)
	if err == nil || !strings.Contains(err.Error(), want) {
		c.t.Fatalf("rflib %s: err = %v, want containing %q", strings.Join(args, " "), err, want)
	}
}

func (c *cli) channel(id string) channelOut {
	c.t.Helper()
	var ch channelOut
	if err := json.Unmarshal([]byte(c.ok("channel", "show", "--id", id, "--format", "json")), &ch); err != nil {
		c.t.Fatal(err)
	}
	return ch
}

func TestChannelUpdateOnlyChangesSuppliedFlags(t *testing.T) {
	c := newCLI(t)
	id := c.ok("channel", "add", "--name", "Repeater", "--rx", "146.940", "--tx", "146.340", "--mode", "NFM",
		"--rx-tone", "ctcss:100.0", "--tx-tone", "ctcss:100.0", "--tone-squelch", "--skip", "--tags", "ham,repeater")

	c.ok("channel", "update", "--id", id, "--power", "high")

	ch := c.channel(id)
	if ch.Power != "high" {
		t.Errorf("power = %q", ch.Power)
	}
	if ch.Mode != "NFM" || ch.RXTone != "ctcss:100.000" || ch.TXTone != "ctcss:100.000" || !ch.ToneSquelch || !ch.Skip {
		t.Errorf("update reset unsupplied fields: %+v", ch)
	}
	if ch.TransmitMHz == nil || len(ch.Tags) != 2 {
		t.Errorf("update dropped tx or tags: %+v", ch)
	}

	c.ok("channel", "update", "--id", id, "--skip=false")
	if c.channel(id).Skip {
		t.Error("explicit --skip=false was ignored")
	}
}

func TestChannelUpdateClearsWithEmptyValues(t *testing.T) {
	c := newCLI(t)
	c.ok("group", "add", "--name", "Metro")
	id := c.ok("channel", "add", "--name", "Repeater", "--rx", "146.940", "--tx", "146.340", "--group", "Metro", "--tags", "ham")

	c.ok("channel", "update", "--id", id, "--tx", "", "--group", "", "--tags", "")

	ch := c.channel(id)
	if ch.TransmitHz != nil || ch.Group != nil || len(ch.Tags) != 0 {
		t.Fatalf("fields not cleared: %+v", ch)
	}
}

func TestGroupDeleteByNameUngroupsChannels(t *testing.T) {
	c := newCLI(t)
	c.ok("group", "add", "--name", "Metro", "--lat", "45.5", "--lon", "-122.6")
	id := c.ok("channel", "add", "--name", "Simplex", "--rx", "146.520", "--group", "Metro")

	c.fails("requires --yes", "group", "delete", "--id", "Metro")
	c.ok("group", "delete", "--id", "Metro", "--yes")

	if ch := c.channel(id); ch.Group != nil {
		t.Fatalf("channel still grouped: %+v", ch.Group)
	}
}

func TestGroupUpdateKeepsUnsuppliedFields(t *testing.T) {
	c := newCLI(t)
	c.ok("group", "add", "--name", "Metro", "--lat", "45.5", "--lon", "-122.6", "--radius-km", "40", "--notes", "keep")
	c.ok("group", "update", "--id", "Metro", "--description", "Local area")

	var g groupOut
	if err := json.Unmarshal([]byte(c.ok("group", "show", "--id", "Metro", "--format", "json")), &g); err != nil {
		t.Fatal(err)
	}
	if g.Description != "Local area" || g.Notes != "keep" || g.Latitude == nil || g.RadiusKM == nil || *g.RadiusKM != 40 {
		t.Fatalf("group = %+v", g)
	}

	c.ok("group", "update", "--id", "Metro", "--lat", "", "--lon", "")
	var cleared groupOut
	if err := json.Unmarshal([]byte(c.ok("group", "show", "--id", "Metro", "--format", "json")), &cleared); err != nil {
		t.Fatal(err)
	}
	if cleared.Latitude != nil || cleared.Longitude != nil {
		t.Fatalf("coordinates not cleared: %+v", cleared)
	}
}

func TestListFiltersByGroupName(t *testing.T) {
	c := newCLI(t)
	c.ok("group", "add", "--name", "Metro")
	c.ok("channel", "add", "--name", "In", "--rx", "146.520", "--group", "Metro")
	c.ok("channel", "add", "--name", "Out", "--rx", "146.550")

	out := c.ok("channel", "list", "--group", "Metro")
	if !strings.Contains(out, "In") || strings.Contains(out, "Out") {
		t.Fatalf("list output = %q", out)
	}
	c.fails("not found", "channel", "list", "--group", "Nowhere")
}

func TestExportToStdoutAndFile(t *testing.T) {
	c := newCLI(t)
	c.ok("channel", "add", "--name", "Calling", "--rx", "146.520", "--tags", "ham")

	stdout, stderr, err := c.exec("export", "csv")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(stdout, "id,name,") || !strings.Contains(stdout, "146.520000") || stderr != "" {
		t.Fatalf("stdout = %q, stderr = %q", stdout, stderr)
	}

	path := filepath.Join(t.TempDir(), "out.csv")
	_, stderr, err = c.exec("export", "csv", "--output", path)
	if err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != stdout {
		t.Fatalf("file export differs from stdout export: %v", err)
	}
	if !strings.Contains(stderr, "Exported 1 channels") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestErrors(t *testing.T) {
	c := newCLI(t)
	c.fails("required flag", "channel", "add", "--name", "No RX")
	c.fails("unknown format", "channel", "list", "--format", "yaml")
	c.fails("not found", "channel", "show", "--id", "missing")
	c.fails("tone squelch requires a receive tone", "channel", "add", "--name", "x", "--rx", "146.52", "--tone-squelch")
}

func TestMissingLibraryIsNotCreated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent.db")
	err := run([]string{"--db", path, "group", "list"}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "run rflib init") {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("a read command created the library file")
	}
}

func TestDBPathFromEnvironment(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("RFLIB_DB", filepath.Join(dir, "env.db"))
	if err := run([]string{"init"}, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "env.db")); err != nil {
		t.Fatal(err)
	}

	t.Setenv("RFLIB_DB", "")
	t.Setenv("XDG_DATA_HOME", dir)
	if err := run([]string{"init"}, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "rflib", "library.db")); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "input")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestImportCSV(t *testing.T) {
	c := newCLI(t)
	file := writeFile(t, "name,receive_mhz,group_name\nCalling,146.520,Metro\nBroken,not-a-number,\n")

	_, stderr, err := c.exec("import", "csv", file)
	if err == nil || !strings.Contains(err.Error(), "1 invalid rows; nothing imported") || !strings.Contains(stderr, "row 3") {
		t.Fatalf("err = %v, stderr = %q", err, stderr)
	}
	if out := c.ok("channel", "list"); out != "" {
		t.Fatalf("aborted import wrote channels: %q", out)
	}

	out := c.ok("import", "csv", file, "--skip-invalid", "--dry-run")
	if !strings.Contains(out, "Dry run") || !strings.Contains(out, "created 1") || !strings.Contains(out, "New groups: Metro") {
		t.Fatalf("dry run output = %q", out)
	}
	if got := c.ok("group", "list"); got != "" {
		t.Fatalf("dry run created groups: %q", got)
	}

	out = c.ok("import", "csv", file, "--skip-invalid", "--tags", "imported")
	if !strings.Contains(out, "created 1, updated 0, skipped 0 duplicates, skipped 1 invalid") {
		t.Fatalf("import output = %q", out)
	}
	if got := c.ok("channel", "list", "--tag", "imported", "--group", "Metro"); !strings.Contains(got, "Calling") {
		t.Fatalf("imported channel missing: %q", got)
	}

	c.fails("unknown duplicate policy", "import", "csv", file, "--on-duplicate", "merge")
	c.fails("unknown match rule", "import", "csv", file, "--match", "name")
}

func TestExportJSONFiltersGroups(t *testing.T) {
	c := newCLI(t)
	c.ok("group", "add", "--name", "Metro")
	c.ok("group", "add", "--name", "Unused")
	c.ok("channel", "add", "--name", "Calling", "--rx", "146.520", "--group", "Metro", "--tags", "ham")

	all := c.ok("export", "json")
	if !strings.Contains(all, `"Unused"`) {
		t.Fatalf("unfiltered backup should include every group: %s", all)
	}
	filtered := c.ok("export", "json", "--tag", "ham")
	if strings.Contains(filtered, `"Unused"`) || !strings.Contains(filtered, `"Metro"`) {
		t.Fatalf("filtered backup should include only referenced groups: %s", filtered)
	}

	restored := newCLI(t)
	out := restored.ok("import", "json", writeFile(t, all))
	if !strings.Contains(out, "created 1") {
		t.Fatalf("restore output = %q", out)
	}
	if got := restored.ok("export", "json"); got != all {
		t.Fatalf("restore is not identical:\n%s\n---\n%s", all, got)
	}
}

func TestExportCHIRP(t *testing.T) {
	c := newCLI(t)
	c.ok("group", "add", "--name", "Alpha")
	c.ok("channel", "add", "--name", "Zulu", "--rx", "146.520", "--tx", "146.520", "--group", "Alpha")
	c.ok("channel", "add", "--name", "Mike", "--rx", "446.000", "--tx", "446.000")
	c.ok("channel", "add", "--name", "Alpha Rpt", "--rx", "147.000", "--tx", "147.600", "--tx-tone", "ctcss:100")
	c.ok("channel", "add", "--name", "Odd", "--rx", "145.000", "--tx-tone", "ctcss:100.5")

	_, _, err := c.exec("export", "chirp")
	if err == nil || !strings.Contains(err.Error(), `"Odd"`) {
		t.Fatalf("err = %v", err)
	}

	names := func(args ...string) string {
		t.Helper()
		out, stderr, err := c.exec(append([]string{"export", "chirp", "--skip-invalid"}, args...)...)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(stderr, `skipped "Odd"`) {
			t.Fatalf("stderr = %q", stderr)
		}
		var got []string
		for _, line := range strings.Split(strings.TrimSpace(out), "\r\n")[1:] {
			f := strings.Split(line, ",")
			got = append(got, f[0]+":"+f[1])
		}
		return strings.Join(got, " ")
	}
	if got := names(); got != "1:Alpha Rpt 2:Mike 3:Zulu" {
		t.Errorf("name sort = %s", got)
	}
	if got := names("--sort", "freq", "--start", "0"); got != "0:Zulu 1:Alpha Rpt 2:Mike" {
		t.Errorf("freq sort = %s", got)
	}
	if got := names("--sort", "group", "--max-name-len", "4"); got != "1:Zulu 2:ARpt 3:Mike" {
		t.Errorf("group sort = %s", got)
	}
	c.fails("unknown sort", "export", "chirp", "--sort", "power")
}

func TestImportCHIRP(t *testing.T) {
	c := newCLI(t)
	file := writeFile(t, "Location,Name,Frequency,Duplex,Offset,Tone,rToneFreq,cToneFreq,DtcsCode,DtcsPolarity,RxDtcsCode,CrossMode,Mode,TStep,Skip,Power,Comment,URCALL,RPT1CALL,RPT2CALL,DVCODE\r\n"+
		"1,Metro,146.940000,-,0.600000,TSQL,100.0,100.0,023,NN,023,Tone->Tone,FM,5.00,,50W,,,,,\r\n")
	if out := c.ok("import", "chirp", file, "--tags", "chirp"); !strings.Contains(out, "created 1") {
		t.Fatalf("import output = %q", out)
	}
	if out := c.ok("import", "chirp", file); !strings.Contains(out, "skipped 1 duplicate") {
		t.Fatalf("re-import output = %q", out)
	}
	id := strings.Fields(c.ok("channel", "list", "--tag", "chirp"))[0]
	ch := c.channel(id)
	if *ch.TransmitMHz != "146.340000" || ch.RXTone != "ctcss:100.000" || !ch.ToneSquelch || ch.Power != "" { // CHIRP watts describe the source radio
		t.Fatalf("channel = %+v", ch)
	}
}

func TestSeedInstallIsIdempotent(t *testing.T) {
	c := newCLI(t)
	if out := c.ok("seed", "list"); !strings.Contains(out, "us-gmrs\t30 channels") {
		t.Fatalf("seed list = %q", out)
	}
	out := c.ok("seed", "install", "us-gmrs", "us-noaa")
	if !strings.Contains(out, "us-gmrs: created 30") || !strings.Contains(out, "us-noaa: created 7") {
		t.Fatalf("first install = %q", out)
	}
	out = c.ok("seed", "install", "us-gmrs", "us-noaa")
	if !strings.Contains(out, "us-gmrs: created 0, updated 0, skipped 30 duplicates") || !strings.Contains(out, "us-noaa: created 0") {
		t.Fatalf("second install = %q", out)
	}
	if out := c.ok("channel", "list", "--service", "noaa"); strings.Count(out, "\n")+1 != 7 {
		t.Fatalf("noaa channels = %q", out)
	}
	c.ok("check") // built-in lists fit their own band plans
	c.fails("unknown list", "seed", "install", "us-pmr446")
}

func TestServiceDefaultsAndWarnings(t *testing.T) {
	c := newCLI(t)
	id := c.ok("channel", "add", "--name", "Dispatch", "--rx", "460.275", "--tx", "465.275", "--service", "public_safety")
	if ch := c.channel(id); !ch.RXOnly || ch.Service != "public_safety" || ch.TransmitMHz == nil {
		t.Fatalf("public safety channel = %+v", ch)
	}
	csv := c.ok("export", "chirp", "--service", "public_safety")
	if !strings.Contains(csv, ",Dispatch,460.275000,off,") {
		t.Fatalf("rx-only channel exported as transmitting: %s", csv)
	}

	_, stderr, err := c.exec("channel", "add", "--name", "Oops", "--rx", "146.52", "--tx", "151.82", "--service", "amateur")
	if err != nil || !strings.Contains(stderr, "warning: Oops: transmit frequency 151.820000 MHz is outside the US amateur bands") {
		t.Fatalf("err = %v, stderr = %q", err, stderr)
	}
	out, _, err := c.exec("check")
	if err == nil || !strings.Contains(out, "Oops") || strings.Contains(out, "Dispatch") {
		t.Fatalf("check: err = %v, out = %q", err, out)
	}

	// Explicitly allowing transmit on a public safety channel is warned about.
	_, stderr, _ = c.exec("channel", "update", "--id", id, "--rx-only=false")
	if !strings.Contains(stderr, "--rx-only") {
		t.Fatalf("stderr = %q", stderr)
	}
	c.fails("unknown service", "channel", "add", "--name", "x", "--rx", "146.52", "--service", "ham")
}

func TestImportWithService(t *testing.T) {
	c := newCLI(t)
	file := writeFile(t, "name,receive_mhz,transmit_mhz\nDispatch,460.275,465.275\n")
	c.ok("import", "csv", file, "--service", "public_safety")
	id := strings.Fields(c.ok("channel", "list", "--service", "public_safety"))[0]
	if ch := c.channel(id); !ch.RXOnly {
		t.Fatalf("imported public safety channel not rx-only: %+v", ch)
	}
}

func TestProfileCommands(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	c := newCLI(t)
	if out := c.ok("profile", "list"); !strings.Contains(out, "yaesu-ftm150\t") || !strings.Contains(out, "(built-in)") {
		t.Fatalf("profile list = %q", out)
	}
	if out := c.ok("profile", "show", "yaesu-ftm6000"); !strings.Contains(out, "max_name_length: 6") {
		t.Fatalf("profile show = %q", out)
	}
	bad := writeFile(t, "name: x\ncolumns: [{name: A, source: nope}]\n")
	c.fails("unknown source", "profile", "validate", bad)

	c.ok("channel", "add", "--name", "Metro Repeater", "--rx", "146.94", "--tx", "146.34", "--tx-tone", "ctcss:127.3", "--power", "low")
	c.ok("channel", "add", "--name", "Club TSQL", "--rx", "147.00", "--tx", "147.60", "--tx-tone", "ctcss:100", "--rx-tone", "ctcss:100", "--tone-squelch")

	_, _, err := c.exec("export", "profile", "yaesu-ftm6000")
	if err == nil || !strings.Contains(err.Error(), "tone squelch") {
		t.Fatalf("err = %v", err)
	}
	out, stderr, err := c.exec("export", "profile", "yaesu-ftm6000", "--skip-invalid", "--start", "5")
	if err != nil || !strings.Contains(stderr, `skipped "Club TSQL"`) {
		t.Fatalf("err = %v, stderr = %q", err, stderr)
	}
	first := strings.SplitN(out, "\n", 2)[0]
	if first != "5,146.94000,146.34000,0.60,-RPT,FM,ON,MtrRpt,TONE,127.3 Hz,023,1500 Hz,LOW,YES,5.0KHz,OFF,OFF,,0" {
		t.Fatalf("first row = %q", first)
	}

	other := newCLI(t)
	if got := other.ok("import", "profile", "yaesu-ftm6000", writeFile(t, out)); !strings.Contains(got, "created 1") {
		t.Fatalf("import = %q", got)
	}
	if got := other.ok("channel", "list"); !strings.Contains(got, "MtrRpt\t146.940000") {
		t.Fatalf("imported channels = %q", got)
	}
	c.fails("unknown profile", "export", "profile", "kenwood")
}

func TestNamesAndShortNames(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	c := newCLI(t)
	id := c.ok("channel", "add", "--name", "Santa Barbara Sheriff Dispatch", "--rx", "460.275", "--short-names", "SB Sheriff Disp, SBSO Disp")
	for _, hz := range []string{"464.650", "464.750"} {
		c.ok("channel", "add", "--name", "RitzCarlton Ops", "--rx", hz)
	}
	if ch := c.channel(id); strings.Join(ch.ShortNames, "|") != "SB Sheriff Disp|SBSO Disp" {
		t.Fatalf("short names = %q", ch.ShortNames)
	}

	out, stderr, err := c.exec("names", "--max-len", "10")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"RtCrltnOps\tshortened\tRitzCarlton Ops", "SBSO Disp\tvariant\tSanta Barbara Sheriff Dispatch"} {
		if !strings.Contains(out, want) {
			t.Errorf("names output missing %q:\n%s", want, out)
		}
	}
	if !strings.Contains(stderr, `duplicate names on the radio: "RtCrltnOps" (2)`) {
		t.Errorf("stderr = %q", stderr)
	}

	out = c.ok("names", "--profile", "yaesu-ftm6000", "--dedupe", "freq")
	if !strings.Contains(out, "RCOp65\tshortened+") || !strings.Contains(out, "RCOp75\tshortened+") || !strings.Contains(out, "SBSODs\tshortened") {
		t.Errorf("profile names = %s", out)
	}

	csv, stderr, err := c.exec("export", "chirp", "--max-name-len", "8", "--dedupe", "index")
	if err != nil || strings.Contains(stderr, "duplicate") {
		t.Fatalf("err = %v, stderr = %q", err, stderr)
	}
	for _, want := range []string{",RCrlOps1,", ",RCrlOps2,", ",SBSODisp,"} {
		if !strings.Contains(csv, want) {
			t.Errorf("CHIRP export missing %q:\n%s", want, csv)
		}
	}

	c.ok("channel", "update", "--id", id, "--short-names", "")
	if ch := c.channel(id); len(ch.ShortNames) != 0 {
		t.Fatalf("short names not cleared: %q", ch.ShortNames)
	}
	c.fails("unknown dedupe", "names", "--dedupe", "suffix")
	c.fails("--charset", "names", "--charset", "Z-A")
}

func TestRadioWorkflow(t *testing.T) {
	c := newCLI(t)
	c.ok("seed", "install", "us-gmrs", "us-noaa")
	ids := map[string]string{}
	for _, line := range strings.Split(c.ok("channel", "list"), "\n") {
		f := strings.Split(line, "\t")
		ids[f[1]] = f[0]
	}
	if out := c.ok("model", "list"); !strings.Contains(out, "radioddity-uv5g\t") || !strings.Contains(out, "CHIRP: Radioddity UV-5G") {
		t.Fatalf("model list = %q", out)
	}
	c.fails("unknown radio model", "radio", "add", "HT", "--model", "uv-5r")
	c.ok("radio", "add", "HT", "--model", "radioddity-uv5g", "--default-power", "high")
	c.ok("radio", "sources", "add", "HT", "service:noaa")

	if out := c.ok("radio", "channels", "add", "HT", "--tag", "repeater", "--sort", "freq"); out != "Added 8 channels to HT" {
		t.Fatalf("add by filter = %q", out)
	}
	c.ok("radio", "channels", "add", "HT", "--id", ids["GMRS 1"]+","+ids["GMRS 8"])
	if out := c.ok("radio", "channels", "add", "HT", "--id", ids["GMRS 1"]); out != "Added 0 channels to HT" {
		t.Fatalf("re-adding = %q", out)
	}

	// Move GMRS 1 to the top, and start GMRS 8 a section at memory 20.
	c.ok("radio", "channels", "move", "HT", "--id", ids["GMRS 1"], "--top")
	c.ok("radio", "channels", "set", "HT", "--id", ids["GMRS 8"], "--section-start", "20", "--scan", "no", "--name", "Kids")
	c.fails("already reach memory", "radio", "channels", "set", "HT", "--id", ids["GMRS 15R"], "--section-start", "0")
	c.ok("radio", "channels", "set", "HT", "--id", ids["GMRS 15R"], "--section-start", "")

	out, stderr, err := c.exec("radio", "channels", "list", "HT")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 10 {
		t.Fatalf("listing:\n%s", out)
	}
	for _, want := range []string{
		"0\tGMRS 1\tfits\tHigh\tscan\t462.562500\tGMRS 1\t",  // capped at 5 W: High (4 W) is fine
		"1\tGMRS15R\tshortened\tHigh\tscan\t462.550000",      // 7 characters
		"§20\tKIDS*\tfits\tLow\tskip*\t467.562500\tGMRS 8\t", // 0.5 W cap: lowest level, warned
	} {
		if !strings.Contains(out, want) {
			t.Errorf("listing missing %q:\n%s", want, out)
		}
	}
	if !strings.Contains(stderr, "lowest level, Low, is above the gmrs limit of 0.5 W") {
		t.Errorf("stderr = %q", stderr)
	}

	if got := c.ok("radio", "sync", "HT", "--dry-run"); strings.Count(got, "would add") != 7 {
		t.Fatalf("sync dry run = %q", got)
	}
	if got := c.ok("radio", "sync", "HT"); !strings.HasSuffix(got, "Added 7 channels to HT") {
		t.Fatalf("sync = %q", got)
	}
	if got := c.ok("radio", "sync", "HT"); got != "Added 0 channels to HT" {
		t.Fatalf("second sync = %q", got)
	}

	csv, _, err := c.exec("radio", "export", "HT")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"\r\n0,GMRS 1,462.562500,,", ",4W,", "\r\n20,KIDS,467.562500,", ",1W,", ",S,"} {
		if !strings.Contains(csv, want) {
			t.Errorf("CHIRP export missing %q:\n%s", want, csv)
		}
	}
	if !strings.Contains(c.ok("radio", "list"), "HT\tradioddity-uv5g\t17 channels") {
		t.Errorf("radio list = %q", c.ok("radio", "list"))
	}

	// Deleting a channel takes it off the radio.
	c.ok("channel", "delete", "--id", ids["GMRS 1"], "--yes")
	if strings.Contains(c.ok("radio", "channels", "list", "HT"), "\tGMRS 1\t") {
		t.Error("deleted channel still on the radio")
	}
	c.fails("requires --yes", "radio", "delete", "HT")
	c.ok("radio", "delete", "HT", "--yes")
	c.fails("not found", "radio", "show", "HT")
}

func TestRadioMoveBeforeAfter(t *testing.T) {
	c := newCLI(t)
	var ids []string
	for _, n := range []string{"A", "B", "C", "D"} {
		ids = append(ids, c.ok("channel", "add", "--name", n, "--rx", "146.52"))
	}
	c.ok("radio", "add", "Truck", "--model", "yaesu-ftm150")
	c.ok("radio", "channels", "add", "Truck", "--id", strings.Join(ids, ","))
	order := func() string {
		var names []string
		for _, l := range strings.Split(c.ok("radio", "channels", "list", "Truck", "--format", "json"), "\"channel_name\": \"")[1:] {
			names = append(names, l[:1])
		}
		return strings.Join(names, "")
	}
	for _, step := range []struct {
		args []string
		want string
	}{
		{[]string{"--id", ids[3], "--before", ids[1]}, "ADBC"},
		{[]string{"--id", ids[0], "--after", ids[2]}, "DBCA"},
		{[]string{"--id", ids[1], "--to", "4"}, "DCAB"},
		{[]string{"--id", ids[3], "--bottom"}, "CABD"},
	} {
		c.ok(append([]string{"radio", "channels", "move", "Truck"}, step.args...)...)
		if got := order(); got != step.want {
			t.Fatalf("after %v: %s, want %s", step.args, got, step.want)
		}
	}
	c.fails("not on Truck", "radio", "channels", "move", "Truck", "--id", "nope", "--top")
}

func TestSearchAndSavedSearches(t *testing.T) {
	c := newCLI(t)
	c.ok("seed", "install", "us-gmrs", "us-noaa", "us-ham-calling")
	count := func(args ...string) int {
		out := c.ok(append([]string{"channel", "list"}, args...)...)
		if out == "" {
			return 0
		}
		return len(strings.Split(out, "\n"))
	}
	all := count()
	if n := count("band:gmrs", "is:repeater"); n != 8 {
		t.Errorf("GMRS repeaters = %d", n)
	}
	if n := count("--query", "band:gmrs is:repeater"); n != 8 {
		t.Errorf("--query = %d", n)
	}
	// A leading negation needs -- so it isn't read as a flag.
	c.fails("unknown shorthand flag", "channel", "list", "-service:gmrs")
	if n := count("--", "-service:gmrs"); n != all-30 {
		t.Errorf("negation = %d of %d", n, all)
	}
	if n := count("band:2m", "OR", "band:70cm"); n == 0 {
		t.Error("OR matched nothing")
	}
	if n := count("--service", "noaa", "is:rxonly"); n != 7 {
		t.Errorf("shorthand plus query = %d", n)
	}
	c.fails(`unknown field "frq"`, "channel", "list", "frq:146")
	c.fails("--lat, --lon, and --radius-km", "channel", "list", "--lat", "34")

	c.ok("search", "add", "wx", "service:noaa")
	c.ok("search", "add", "@Quiet", "@wx OR service:gmrs -is:repeater", "--notes", "listen")
	c.fails("already exists", "search", "add", "wx", "band:noaa")
	c.fails("uses itself", "search", "add", "loop", "@loop")
	c.fails("no saved search", "search", "add", "bad", "@missing")
	c.fails("must be lower-case", "search", "add", "Bad Name!", "tag:x")
	if n := count("@quiet"); n != 7+22 {
		t.Errorf("@quiet = %d", n)
	}
	if out := c.ok("search", "list"); !strings.Contains(out, "@quiet\t29\t") || !strings.Contains(out, "@wx\t7\tservice:noaa") {
		t.Errorf("search list = %q", out)
	}
	c.fails("uses itself (@wx → @quiet → @wx)", "search", "update", "wx", "--query", "@quiet")
	c.fails("used by @quiet", "search", "delete", "wx", "--yes")
	c.fails("used by @quiet", "search", "update", "wx", "--name", "weather")
	c.ok("radio", "add", "HT", "--model", "baofeng-uv5rm")
	c.ok("radio", "sources", "add", "HT", "@quiet")
	if out := c.ok("search", "show", "quiet"); !strings.Contains(out, "used by: radio HT source 1") || !strings.Contains(out, "matches: 29") {
		t.Errorf("search show = %q", out)
	}
	c.fails("used by radio HT source 1", "search", "delete", "quiet", "--yes")
	c.ok("radio", "sources", "remove", "HT", "1")
	c.ok("search", "delete", "quiet", "--yes")
	c.ok("search", "update", "wx", "--name", "weather", "--query", "band:noaa")
	if n := count("@weather"); n != 7 {
		t.Errorf("renamed search = %d", n)
	}
	if n := count("radio:none"); n != all {
		t.Errorf("radio:none = %d", n)
	}
}

func TestRadioSources(t *testing.T) {
	c := newCLI(t)
	c.ok("seed", "install", "us-gmrs", "us-noaa")
	c.ok("radio", "add", "HT", "--model", "baofeng-uv5rm")
	c.fails("no sources", "radio", "sync", "HT")
	c.ok("radio", "sources", "add", "HT", "band:noaa", "--sort", "freq", "--section-start", "100")
	c.ok("radio", "sources", "add", "HT", "band:gmrs -is:repeater", "--sort", "freq")
	c.fails("unknown field", "radio", "sources", "add", "HT", "frq:1")
	c.fails("unknown sort", "radio", "sources", "add", "HT", "tag:x", "--sort", "size")
	// Make GMRS first: it claims its channels first and goes at the top.
	c.ok("radio", "sources", "move", "HT", "2", "--to", "1")
	if got := c.ok("radio", "sync", "HT"); !strings.Contains(got, "Source 1 (band:gmrs -is:repeater): added 22") || !strings.HasSuffix(got, "Added 29 channels to HT") {
		t.Fatalf("sync = %q", got)
	}
	// A hand-added channel goes at the end and stays there.
	hand := c.ok("channel", "add", "--name", "Hand", "--rx", "146.52")
	c.ok("radio", "channels", "add", "HT", "--id", hand)
	// New matches for the first source go after its channels, not at the end.
	newGMRS := c.ok("channel", "add", "--name", "AAA New", "--rx", "462.7125", "--service", "gmrs")
	c.ok("radio", "sync", "HT")

	type row struct {
		Memory       int    `json:"memory"`
		SectionStart bool   `json:"section_start"`
		ChannelID    string `json:"channel_id"`
		ChannelName  string `json:"channel_name"`
		ReceiveMHz   string `json:"receive_mhz"`
	}
	var rows []row
	if err := json.Unmarshal([]byte(c.ok("radio", "channels", "list", "HT", "--format", "json")), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 31 {
		t.Fatalf("%d memories", len(rows))
	}
	if rows[0].Memory != 1 || rows[0].ReceiveMHz != "462.550000" {
		t.Errorf("first = %+v", rows[0])
	}
	if rows[22].ChannelID != newGMRS || rows[22].Memory != 23 {
		t.Errorf("new GMRS match = %+v, want memory 23", rows[22])
	}
	if !rows[23].SectionStart || rows[23].Memory != 100 || rows[23].ReceiveMHz != "162.400000" {
		t.Errorf("NOAA section = %+v", rows[23])
	}
	if rows[30].ChannelID != hand || rows[30].Memory != 107 {
		t.Errorf("hand-added = %+v", rows[30])
	}

	if out := c.ok("radio", "sources", "list", "HT"); !strings.Contains(out, "1\t-\tsort freq\t23 channels\tband:gmrs -is:repeater") ||
		!strings.Contains(out, "2\t§100\tsort freq\t7 channels\tband:noaa") {
		t.Errorf("sources list = %q", out)
	}
	if out := c.ok("radio", "show", "HT"); !strings.Contains(out, "sources:\n1\t") {
		t.Errorf("radio show = %q", out)
	}
	// Re-sorting by name puts numbers in order; the NOAA section and the
	// hand-added channel stay where they are.
	c.ok("radio", "sources", "set", "HT", "1", "--sort", "name", "--keep-sorted")
	if out := c.ok("radio", "sources", "list", "HT"); !strings.Contains(out, "1\t-\tsort name, kept sorted\t23 channels") {
		t.Errorf("sources list = %q", out)
	}
	if got := c.ok("radio", "sync", "HT", "--dry-run"); !strings.Contains(got, "would re-sort\tsource 1\t") || strings.Contains(got, "source 2\t") {
		t.Errorf("dry run = %q", got)
	}
	if got := c.ok("radio", "sync", "HT"); !strings.Contains(got, "Source 1 (band:gmrs -is:repeater): re-sorted") || !strings.HasSuffix(got, "program the radio again") {
		t.Errorf("sync = %q", got)
	}
	rows = nil
	if err := json.Unmarshal([]byte(c.ok("radio", "channels", "list", "HT", "--format", "json")), &rows); err != nil {
		t.Fatal(err)
	}
	if rows[0].ChannelName != "AAA New" || rows[1].ChannelName != "GMRS 1" || rows[2].ChannelName != "GMRS 2" || rows[10].ChannelName != "GMRS 10" ||
		rows[22].ChannelName != "GMRS 22" || rows[23].Memory != 100 || rows[30].ChannelID != hand {
		t.Errorf("after re-sort: %+v", rows)
	}
	if got := c.ok("radio", "sync", "HT"); got != "Added 0 channels to HT" {
		t.Errorf("sorted source re-sorted again: %q", got)
	}
	c.ok("radio", "sources", "set", "HT", "1", "--keep-sorted=false")

	// Moving the section start applies now; removing it clears it.
	c.ok("radio", "sources", "set", "HT", "2", "--section-start", "50")
	if !strings.Contains(c.ok("radio", "channels", "list", "HT"), "§50\t") {
		t.Error("section start not moved")
	}
	c.fails("already reach memory", "radio", "sources", "set", "HT", "2", "--section-start", "5")
	c.ok("radio", "sources", "set", "HT", "2", "--section-start", "")
	if strings.Contains(c.ok("radio", "channels", "list", "HT"), "§") {
		t.Error("section start not cleared")
	}
	c.fails("has sources 1-2", "radio", "sources", "remove", "HT", "3")
	if out := c.ok("radio", "sources", "remove", "HT", "2", "--with-channels"); out != "Removed source 2 from HT and 7 channels" {
		t.Errorf("remove = %q", out)
	}
	c.ok("radio", "sources", "remove", "HT", "1")
	if out := c.ok("radio", "list"); !strings.Contains(out, "24 channels") {
		t.Errorf("channels kept after removing a source: %q", out)
	}
}

func TestRadioSkipsOutOfBandChannels(t *testing.T) {
	c := newCLI(t)
	c.ok("seed", "install", "us-cb", "us-noaa")
	c.ok("radio", "add", "HT", "--model", "radioddity-uv5g") // 130-176 and 400-520 MHz
	if n := len(strings.Split(c.ok("channel", "list", "tunes:HT"), "\n")); n != 7 {
		t.Errorf("tunes:HT = %d channels", n)
	}
	if n := len(strings.Split(c.ok("channel", "list", "tunes:yaesu-ftm150"), "\n")); n != 7 {
		t.Errorf("tunes:yaesu-ftm150 = %d channels", n)
	}
	c.fails("no radio or radio model", "channel", "list", "tunes:nothing")

	out, stderr, err := c.exec("radio", "channels", "add", "HT", "-q", "service:cb OR service:noaa")
	if err != nil {
		t.Fatal(err)
	}
	if out = strings.TrimSpace(out); out != "Added 7 channels to HT; skipped 40 outside its bands" {
		t.Errorf("add = %q", out)
	}
	if !strings.Contains(stderr, `skipped "CB 19" (27.185000 MHz): HT can't receive it`) {
		t.Errorf("stderr = %q", stderr)
	}
	cb19 := strings.Split(c.ok("channel", "list", "name:\"CB 19\""), "\t")[0]
	if out := c.ok("radio", "channels", "add", "HT", "--id", cb19, "--any-band"); out != "Added 1 channels to HT" {
		t.Errorf("--any-band = %q", out)
	}

	c.ok("radio", "add", "Base", "--model", "radioddity-uv5g")
	c.ok("radio", "sources", "add", "Base", "service:cb OR service:noaa")
	if out := c.ok("radio", "sync", "Base", "--dry-run"); strings.Count(out, "would skip") != 40 || strings.Count(out, "would add") != 7 {
		t.Errorf("dry run = %q", out)
	}
	if out := c.ok("radio", "sync", "Base"); !strings.Contains(out, "added 7; skipped 40 outside its bands") {
		t.Errorf("sync = %q", out)
	}
}

func TestLibraries(t *testing.T) {
	c := newCLI(t)
	if out := c.ok("library", "list"); !strings.HasPrefix(out, "*default\t0 channels\t0 radios\t") {
		t.Fatalf("library list = %q", out)
	}
	c.ok("library", "add", "club", "--description", "Club radios")
	c.fails("already exists", "library", "add", "club")
	c.fails(`no library named "nope"`, "--library", "nope", "channel", "list")

	// Each library has its own groups, channels, radios, and searches, so
	// the same names can be used in both.
	for _, lib := range []string{"default", "club"} {
		c.ok("--library", lib, "group", "add", "--name", "Home")
		c.ok("--library", lib, "radio", "add", "HT", "--model", "baofeng-uv5rm")
		c.ok("--library", lib, "search", "add", "wx", "band:noaa")
	}
	c.ok("seed", "install", "us-noaa")
	clubCh := c.ok("--library", "club", "channel", "add", "--name", "Club Net", "--rx", "147.000", "--group", "Home")
	if out := c.ok("--library", "club", "channel", "list"); !strings.Contains(out, "Club Net") || strings.Contains(out, "NOAA") {
		t.Errorf("club channels = %q", out)
	}
	if out := c.ok("channel", "list"); strings.Contains(out, "Club Net") {
		t.Errorf("default sees club's channel: %q", out)
	}
	c.fails("not found", "channel", "show", "--id", clubCh)
	c.fails("not found", "radio", "channels", "add", "HT", "--id", clubCh)

	// $RFLIB_LIBRARY chooses the library when --library doesn't.
	t.Setenv("RFLIB_LIBRARY", "club")
	if out := c.ok("channel", "list"); !strings.Contains(out, "Club Net") {
		t.Errorf("RFLIB_LIBRARY ignored: %q", out)
	}
	if out := c.ok("library", "list"); !strings.Contains(out, "*club\t1 channels\t1 radios\t") {
		t.Errorf("library list marks the wrong one: %q", out)
	}
	t.Setenv("RFLIB_LIBRARY", "")

	// Copy the default library's channels into the club's: the copies get
	// new IDs, and groups are matched by name.
	backup := filepath.Join(t.TempDir(), "backup.json")
	c.ok("export", "json", "--output", backup)
	if out := c.ok("--library", "club", "import", "json", backup); !strings.Contains(out, "created 7") {
		t.Fatalf("import into club = %q", out)
	}
	defaultIDs := c.ok("channel", "list", "band:noaa")
	clubIDs := c.ok("--library", "club", "channel", "list", "band:noaa")
	if len(strings.Split(clubIDs, "\n")) != 7 || strings.Split(clubIDs, "\t")[0] == strings.Split(defaultIDs, "\t")[0] {
		t.Errorf("copies share IDs:\n%s\n%s", defaultIDs, clubIDs)
	}

	c.ok("library", "update", "club", "--name", "Club Radios")
	c.fails("requires --yes", "library", "delete", "Club Radios")
	c.ok("library", "delete", "Club Radios", "--yes")
	if out := c.ok("library", "list"); strings.Contains(out, "Club") {
		t.Errorf("deleted library listed: %q", out)
	}
	if out := c.ok("channel", "list", "band:noaa"); len(strings.Split(out, "\n")) != 7 {
		t.Errorf("deleting club touched default: %q", out)
	}
}
