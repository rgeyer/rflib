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

	c.ok("channel", "update", "--id", id, "--power", "25W")

	ch := c.channel(id)
	if ch.Power != "25W" {
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
	if got := names("--sort", "group", "--max-name-len", "4"); got != "1:Zulu 2:Alph 3:Mike" {
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
	if *ch.TransmitMHz != "146.340000" || ch.RXTone != "ctcss:100.000" || !ch.ToneSquelch || ch.Power != "50W" {
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
	if first != "5,146.94000,146.34000,0.60,-RPT,FM,ON,Metro,TONE,127.3 Hz,023,1500 Hz,LOW,YES,5.0KHz,OFF,OFF,,0" {
		t.Fatalf("first row = %q", first)
	}

	other := newCLI(t)
	if got := other.ok("import", "profile", "yaesu-ftm6000", writeFile(t, out)); !strings.Contains(got, "created 1") {
		t.Fatalf("import = %q", got)
	}
	if got := other.ok("channel", "list"); !strings.Contains(got, "Metro\t146.940000") {
		t.Fatalf("imported channels = %q", got)
	}
	c.fails("unknown profile", "export", "profile", "kenwood")
}
