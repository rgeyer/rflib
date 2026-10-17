package userfiles

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// thing is a user file kind for the tests: "name: X" on its first line.
func kind(dir string) Kind[string] {
	return Kind[string]{
		What: "thing",
		Dir:  func() (string, error) { return dir, nil },
		Parse: func(b []byte) (string, error) {
			name, ok := strings.CutPrefix(strings.TrimSpace(string(b)), "name: ")
			if !ok || name == "" {
				return "", errors.New("no name")
			}
			return name, nil
		},
		Name: func(s string) string { return s },
	}
}

func write(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScanSkipsWhatCantBeUsed(t *testing.T) {
	dir := t.TempDir()
	k := kind(dir)
	if files, problems := k.Scan(); len(files)+len(problems) != 0 {
		t.Fatalf("an empty directory = %v %v", files, problems)
	}
	write(t, filepath.Join(dir, "a.yaml"), "name: a")
	write(t, filepath.Join(dir, "broken.yaml"), "oops")
	write(t, filepath.Join(dir, "b.yml"), "name: b")
	write(t, filepath.Join(dir, "c.yaml"), "name: a")
	write(t, filepath.Join(dir, "notes.txt"), "not a yaml file")
	files, problems := k.Scan()
	if len(files) != 2 || files[0].Value != "a" || files[1].Value != "b" {
		t.Errorf("files = %+v", files)
	}
	if len(problems) != 2 || filepath.Base(problems[0].Path) != "broken.yaml" || !strings.Contains(problems[1].Error(), "as a.yaml already is") {
		t.Errorf("problems = %v", problems)
	}
	if p, ok := ProblemWith(problems, "broken"); !ok || filepath.Base(p.Path) != "broken.yaml" {
		t.Errorf("ProblemWith = %v %v", p, ok)
	}
}

func TestInstallAndRemove(t *testing.T) {
	dir, src := t.TempDir(), t.TempDir()
	k := kind(filepath.Join(dir, "things"))
	write(t, filepath.Join(src, "sent.yaml"), "name: ht")
	f, old, err := k.Install(filepath.Join(src, "sent.yaml"), false)
	if err != nil || old != "" || f.Path != filepath.Join(dir, "things", "ht.yaml") {
		t.Fatalf("install = %+v %q %v", f, old, err)
	}
	// A second file with the name needs --replace, and replaces the first.
	write(t, filepath.Join(src, "fixed.yaml"), "name: ht")
	if _, _, err := k.Install(filepath.Join(src, "fixed.yaml"), false); !errors.Is(err, ErrInstalled) {
		t.Fatalf("installing over = %v", err)
	}
	if _, old, err = k.Install(filepath.Join(src, "fixed.yaml"), true); err != nil || old != f.Path {
		t.Fatalf("replace = %q %v", old, err)
	}
	// One dropped in by hand under another file name is replaced, and
	// the new one takes the name's file.
	if err := os.Remove(f.Path); err != nil {
		t.Fatal(err)
	}
	byHand := filepath.Join(dir, "things", "my-ht-v1.yaml")
	write(t, byHand, "name: ht")
	if _, old, err = k.Install(filepath.Join(src, "fixed.yaml"), true); err != nil || old != byHand {
		t.Fatalf("reinstall = %q %v", old, err)
	}
	if _, err := os.Stat(byHand); !os.IsNotExist(err) {
		t.Errorf("the hand-placed file is still there: %v", err)
	}
	write(t, filepath.Join(dir, "things", "ht2.yaml"), "name: ht2")
	if _, _, err := k.Install(filepath.Join(src, "missing.yaml"), false); err == nil {
		t.Error("installing a missing file worked")
	}
	write(t, filepath.Join(src, "bad.yaml"), "oops")
	if _, _, err := k.Install(filepath.Join(src, "bad.yaml"), false); err == nil || !strings.Contains(err.Error(), "no name") {
		t.Errorf("installing a bad file = %v", err)
	}

	if _, err := k.Remove("ht"); err != nil {
		t.Fatal(err)
	}
	if files, _ := k.Scan(); len(files) != 1 || files[0].Value != "ht2" {
		t.Errorf("after remove = %+v", files)
	}
	if _, err := k.Remove("ht"); err == nil {
		t.Error("removing a removed file worked")
	}
	// A broken file goes by its file name.
	write(t, filepath.Join(dir, "things", "half-done.yaml"), "oops")
	if _, err := k.Remove("half-done"); err != nil {
		t.Errorf("removing a broken file = %v", err)
	}
}
