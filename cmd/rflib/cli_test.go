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
