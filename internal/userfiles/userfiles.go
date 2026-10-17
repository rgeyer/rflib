// Package userfiles reads, installs, and removes the user's own YAML files
// of one kind, such as radio models or export profiles, kept in a directory
// that rflib reads beside its built-in ones. Each file declares a name. A
// file that can't be read is skipped and reported, so one broken file
// doesn't stop everything else working.
package userfiles

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Kind is one kind of user file.
type Kind[T any] struct {
	// What names the kind in messages: "model", "profile".
	What string
	// Dir is the directory the files live in.
	Dir func() (string, error)
	// Parse reads and validates a file's contents.
	Parse func([]byte) (T, error)
	// Name is the name a file declares.
	Name func(T) string
}

// File is one usable file.
type File[T any] struct {
	Value T
	Path  string
	Data  []byte
}

// Problem is a file in the directory that can't be used: it doesn't
// parse, or another file already declares its name.
type Problem struct {
	Path string
	Err  error
}

func (p Problem) Error() string { return fmt.Sprintf("%s: %v", p.Path, p.Err) }

// Scan reads every .yaml and .yml file in the directory, in name order.
// A file that doesn't parse, or that declares a name an earlier file has,
// is a Problem and is left out. A missing directory has no files.
func (k Kind[T]) Scan() ([]File[T], []Problem) {
	dir, err := k.Dir()
	if err != nil {
		return nil, nil
	}
	paths, _ := filepath.Glob(filepath.Join(dir, "*.yaml"))
	more, _ := filepath.Glob(filepath.Join(dir, "*.yml"))
	paths = append(paths, more...)
	slices.Sort(paths)
	var files []File[T]
	var problems []Problem
	byName := map[string]string{}
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			problems = append(problems, Problem{p, err})
			continue
		}
		v, err := k.Parse(data)
		if err != nil {
			problems = append(problems, Problem{p, err})
			continue
		}
		name := k.Name(v)
		if first, ok := byName[name]; ok {
			problems = append(problems, Problem{p, fmt.Errorf("it's named %s, as %s already is; remove one", name, filepath.Base(first))})
			continue
		}
		byName[name] = p
		files = append(files, File[T]{v, p, data})
	}
	return files, problems
}

// ProblemWith returns the problem with a file whose name is the name asked
// for, such as my-ht.yaml for my-ht, so a lookup that fails can say why.
func ProblemWith(problems []Problem, name string) (Problem, bool) {
	for _, p := range problems {
		base := filepath.Base(p.Path)
		if strings.TrimSuffix(strings.TrimSuffix(base, ".yaml"), ".yml") == name {
			return p, true
		}
	}
	return Problem{}, false
}

// ErrInstalled is installing a file whose name an installed file already
// has, without replace.
var ErrInstalled = errors.New("already installed")

// Install checks a file and copies it into the directory as NAME.yaml,
// named by what it declares. A file already declaring that name is
// replaced only with replace (and removed, when its file name differs).
// It returns the installed file and the path of the one it replaced.
func (k Kind[T]) Install(path string, replace bool) (File[T], string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return File[T]{}, "", err
	}
	v, err := k.Parse(data)
	if err != nil {
		return File[T]{}, "", fmt.Errorf("%s: %w", path, err)
	}
	name := k.Name(v)
	dir, err := k.Dir()
	if err != nil {
		return File[T]{}, "", err
	}
	dest := filepath.Join(dir, name+".yaml")
	files, _ := k.Scan()
	old := ""
	if i := slices.IndexFunc(files, func(f File[T]) bool { return k.Name(f.Value) == name }); i >= 0 {
		old = files[i].Path
	} else if _, err := os.Stat(dest); err == nil {
		// A broken file has the name; installing a good one replaces it.
		old = dest
	}
	if old != "" && !replace && !samePath(old, path) {
		return File[T]{}, "", fmt.Errorf("a %s named %s is installed from %s; --replace replaces it: %w", k.What, name, old, ErrInstalled)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return File[T]{}, "", err
	}
	if !samePath(dest, path) {
		if err := os.WriteFile(dest, data, 0o644); err != nil {
			return File[T]{}, "", err
		}
	}
	if old != "" && !samePath(old, dest) {
		if err := os.Remove(old); err != nil {
			return File[T]{}, "", err
		}
	}
	if samePath(old, path) {
		old = ""
	}
	return File[T]{v, dest, data}, old, nil
}

// Remove deletes the file declaring name, and returns it.
func (k Kind[T]) Remove(name string) (File[T], error) {
	files, problems := k.Scan()
	for _, f := range files {
		if k.Name(f.Value) == name {
			return f, os.Remove(f.Path)
		}
	}
	// A broken file can be removed by its file name.
	if p, ok := ProblemWith(problems, name); ok {
		return File[T]{Path: p.Path}, os.Remove(p.Path)
	}
	return File[T]{}, fmt.Errorf("no installed %s is named %s", k.What, name)
}

func samePath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	x, err1 := filepath.Abs(a)
	y, err2 := filepath.Abs(b)
	return err1 == nil && err2 == nil && x == y
}
