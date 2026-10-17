package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A tester is sent a model file: they install it, use it, see where it
// comes from, check what's broken, and remove it.
func TestModelInstallAndRemove(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	dir := filepath.Join(cfg, "rflib", "models")
	c := newCLI(t)
	c.fails("no installed model is named nope", "model", "remove", "nope")

	sent := writeFile(t, "name: my-ht\ndescription: A test radio\nformat: chirp\nmemories: {first: 1, last: 50}\nnames: {max_length: 8}\n")
	out, errOut, err := c.exec("model", "install", sent)
	if err != nil || strings.TrimSpace(out) != filepath.Join(dir, "my-ht.yaml") || !strings.Contains(errOut, "Installed the model my-ht") {
		t.Fatalf("install = %q %q %v", out, errOut, err)
	}
	c.fails("--replace", "model", "install", sent)
	if _, errOut, err := c.exec("model", "install", sent, "--replace"); err != nil || !strings.Contains(errOut, "replacing") {
		t.Fatalf("replace = %q %v", errOut, err)
	}
	if out := c.ok("model", "list", "--fields", "name,status"); !strings.Contains(out, "my-ht\tlocal") || !strings.Contains(out, "yaesu-ft857\tbuilt-in") {
		t.Errorf("model list = %q", out)
	}

	c.ok("radio", "add", "HT", "--model", "my-ht")
	if out := c.ok("radio", "show", "HT"); !strings.Contains(out, "model: my-ht (from "+filepath.Join(dir, "my-ht.yaml")+")") {
		t.Errorf("radio show = %q", out)
	}
	if _, errOut, err := c.exec("radio", "export", "HT"); err != nil || !strings.Contains(errOut, "note: model my-ht (from ") {
		t.Errorf("radio export = %q %v", errOut, err)
	}

	// A fix to a built-in replaces it, and says so.
	fix := writeFile(t, "name: yaesu-ft857\ndescription: Fixed\nformat: chirp\nmemories: {first: 1, last: 199}\n")
	if _, errOut, err := c.exec("model", "install", fix); err != nil || !strings.Contains(errOut, "in place of the built-in one") {
		t.Fatalf("install fix = %q %v", errOut, err)
	}
	if out := c.ok("model", "list", "--fields", "name,status"); !strings.Contains(out, "yaesu-ft857\treplaces the built-in") {
		t.Errorf("model list = %q", out)
	}

	// A broken file doesn't stop the rest; validate and list say what's
	// wrong, and using it says why it can't be.
	if err := os.WriteFile(filepath.Join(dir, "half-done.yaml"), []byte("name: half-done\nformat: nope\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, errOut, err := c.exec("model", "list"); err != nil || !strings.Contains(errOut, "warning: skipped "+filepath.Join(dir, "half-done.yaml")) {
		t.Errorf("model list with a broken file = %q %v", errOut, err)
	}
	c.ok("radio", "export", "HT")
	c.fails("half-done.yaml can't be used", "radio", "add", "Other", "--model", "half-done")
	if _, errOut, err := c.exec("model", "validate"); err == nil || !strings.Contains(errOut, "my-ht is valid (local)") || !strings.Contains(err.Error(), "1 model file(s)") {
		t.Errorf("validate all = %q %v", errOut, err)
	}

	if got := c.status("model", "remove", "yaesu-ft857"); !strings.Contains(got, "the built-in model yaesu-ft857 is used again") {
		t.Errorf("remove fix = %q", got)
	}
	c.ok("model", "remove", "half-done")
	c.fails("built-in model", "model", "remove", "yaesu-ft857")
	if _, errOut, err := c.exec("model", "validate"); err != nil || !strings.Contains(errOut, "my-ht is valid") {
		t.Errorf("validate all = %q %v", errOut, err)
	}
}

func TestProfileInstall(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	c := newCLI(t)
	src := c.ok("profile", "show", "yaesu-ftm150")
	if _, errOut, err := c.exec("profile", "install", writeFile(t, src)); err != nil || !strings.Contains(errOut, "it's the same as the built-in profile") {
		t.Fatalf("install = %q %v", errOut, err)
	}
	if _, errOut, err := c.exec("profile", "list"); err != nil || !strings.Contains(errOut, "rflib profile remove yaesu-ftm150 uses the built-in") {
		t.Errorf("profile list = %q %v", errOut, err)
	}
	c.ok("profile", "remove", "yaesu-ftm150")
	if out := c.ok("profile", "list", "--fields", "name,status"); !strings.Contains(out, "yaesu-ftm150\tbuilt-in") {
		t.Errorf("profile list = %q", out)
	}
}
