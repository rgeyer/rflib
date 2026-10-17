// Command dist builds rflib's release archives: rflib for each system it's
// released for, with the web interface built in (build it first, with make
// web), each in an archive with the README and the license, and a
// SHA256SUMS file listing them.
//
//	go run ./tools/dist -version v0.1.0 [-out dist]
package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

// target is a system rflib is released for.
type target struct{ os, arch, arm string }

// targets are the released systems: Linux, macOS, and Windows on x86-64
// and 64-bit ARM, and Linux on 32-bit ARM (ARMv7, such as older Raspberry
// Pis).
var targets = []target{
	{"linux", "amd64", ""},
	{"linux", "arm64", ""},
	{"linux", "arm", "7"},
	{"darwin", "amd64", ""},
	{"darwin", "arm64", ""},
	{"windows", "amd64", ""},
	{"windows", "arm64", ""},
}

// name is the target's part of an archive's name: linux-arm64, linux-armv7,
// macos-arm64, windows-amd64.
func (t target) name() string {
	osName := t.os
	if osName == "darwin" {
		osName = "macos"
	}
	arch := t.arch
	if t.arm != "" {
		arch = "armv" + t.arm
	}
	return osName + "-" + arch
}

var release = regexp.MustCompile(`^v\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$`)

func main() {
	version := flag.String("version", "", "The release, such as v0.1.0")
	out := flag.String("out", "dist", "Directory to write the archives to")
	flag.Parse()
	if err := run(*version, *out); err != nil {
		fmt.Fprintln(os.Stderr, "dist:", err)
		os.Exit(1)
	}
}

func run(version, out string) error {
	if !release.MatchString(version) {
		return fmt.Errorf("-version must be a release such as v0.1.0, not %q", version)
	}
	if _, err := os.Stat(filepath.Join("web", "dist", "index.html")); err != nil {
		return errors.New("the web interface isn't built; run make web first")
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	build, err := os.MkdirTemp("", "rflib-dist-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(build) }()
	var archives []string
	for _, t := range targets {
		bin := filepath.Join(build, t.name(), "rflib")
		if t.os == "windows" {
			bin += ".exe"
		}
		cmd := exec.Command("go", "build", "-trimpath", "-o", bin,
			"-ldflags", "-s -w -X github.com/ryan/rflib/internal/buildinfo.Version="+version, "./cmd/rflib")
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS="+t.os, "GOARCH="+t.arch, "GOARM="+t.arm)
		cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("%s: %w", t.name(), err)
		}
		dir := "rflib-" + version + "-" + t.name()
		files := []file{{bin, filepath.Base(bin), 0o755}, {"README.md", "README.md", 0o644}, {"LICENSE", "LICENSE", 0o644}}
		var name string
		if t.os == "windows" {
			name = dir + ".zip"
			err = writeZip(filepath.Join(out, name), dir, files)
		} else {
			name = dir + ".tar.gz"
			err = writeTarGz(filepath.Join(out, name), dir, files)
		}
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		fmt.Fprintln(os.Stderr, "wrote", filepath.Join(out, name))
		archives = append(archives, name)
	}
	return writeSums(out, archives)
}

// file is one file in an archive: where it is, its name in the archive, and
// its mode.
type file struct {
	path, name string
	mode       int64
}

// stamp is every archived file's time, so archives of the same build are
// the same.
var stamp = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func writeTarGz(path, dir string, files []file) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for _, x := range files {
		data, err := os.ReadFile(x.path)
		if err != nil {
			return err
		}
		h := &tar.Header{Name: dir + "/" + x.name, Mode: x.mode, Size: int64(len(data)), ModTime: stamp, Typeflag: tar.TypeReg, Format: tar.FormatPAX}
		if err := tw.WriteHeader(h); err != nil {
			return err
		}
		if _, err := tw.Write(data); err != nil {
			return err
		}
	}
	return errors.Join(tw.Close(), gz.Close(), f.Close())
}

func writeZip(path, dir string, files []file) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(f)
	for _, x := range files {
		data, err := os.ReadFile(x.path)
		if err != nil {
			return err
		}
		h := &zip.FileHeader{Name: dir + "/" + x.name, Method: zip.Deflate, Modified: stamp}
		h.SetMode(os.FileMode(x.mode))
		w, err := zw.CreateHeader(h)
		if err != nil {
			return err
		}
		if _, err := w.Write(data); err != nil {
			return err
		}
	}
	return errors.Join(zw.Close(), f.Close())
}

// writeSums writes SHA256SUMS, in sha256sum's format, so sha256sum -c
// checks it.
func writeSums(out string, names []string) error {
	slices.Sort(names)
	var b strings.Builder
	for _, n := range names {
		f, err := os.Open(filepath.Join(out, n))
		if err != nil {
			return err
		}
		h := sha256.New()
		_, err = io.Copy(h, f)
		_ = f.Close()
		if err != nil {
			return err
		}
		fmt.Fprintf(&b, "%s  %s\n", hex.EncodeToString(h.Sum(nil)), n)
	}
	return os.WriteFile(filepath.Join(out, "SHA256SUMS"), []byte(b.String()), 0o644)
}
