package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ryan/rflib/internal/app"
	"github.com/ryan/rflib/internal/domain"
	"github.com/ryan/rflib/internal/seed"
	"github.com/ryan/rflib/internal/store"
	"github.com/ryan/rflib/internal/store/sqlite"
	"github.com/spf13/cobra"
)

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// run runs rflib. Data goes to out; status messages, warnings, and errors
// go to errOut, so out can be piped.
func run(args []string, in io.Reader, out, errOut io.Writer) error {
	cmd := newRootCmd(out, errOut)
	cmd.SetIn(in)
	cmd.SetArgs(args)
	return cmd.Execute()
}

// dbPath resolves the library path: --db, then $RFLIB_DB, then the XDG data
// directory.
func dbPath(cmd *cobra.Command) (string, error) {
	if p, _ := cmd.Flags().GetString("db"); p != "" {
		return p, nil
	}
	if p := os.Getenv("RFLIB_DB"); p != "" {
		return p, nil
	}
	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("no --db given and home directory is unknown: %w", err)
		}
		dataHome = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(dataHome, "rflib", "library.db"), nil
}

// withDB opens an existing, initialized database. It never creates one:
// that only happens through rflib init.
func withDB(cmd *cobra.Command, fn func(store.Store) error) error {
	path, err := dbPath(cmd)
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("library %s does not exist; run rflib init --db %s", path, path)
	}
	d, err := sqlite.Open(path)
	if err != nil {
		return err
	}
	defer d.Close()
	if err = d.Ready(cmd.Context()); err != nil {
		return err
	}
	return fn(d)
}

// withApp opens the database and the user's library's services. The shared
// list has its own commands (see withShared).
func withApp(cmd *cobra.Command, fn func(*app.Service) error) error {
	return withDB(cmd, func(d store.Store) error {
		a, err := app.Open(cmd.Context(), d, "")
		if err != nil {
			return err
		}
		return fn(a)
	})
}

func initLibrary(cmd *cobra.Command, _ []string) error {
	path, err := dbPath(cmd)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create library directory: %w", err)
	}
	_, statErr := os.Stat(path)
	created := errors.Is(statErr, fs.ErrNotExist)
	d, err := sqlite.Open(path)
	if err != nil {
		return err
	}
	defer d.Close()
	if err = d.Migrate(cmd.Context()); err != nil {
		return err
	}
	fmt.Fprintln(cmd.ErrOrStderr(), "Library initialized:", path)
	sh, err := app.Open(cmd.Context(), d, domain.SharedLibrary)
	if err != nil {
		return err
	}
	if !created {
		// Upgrading changes no shared channel; it only records which ones
		// came from built-in lists. List changes wait for rflib seed update.
		return sh.RecordSeeds(cmd.Context())
	}
	if noSeed, _ := cmd.Flags().GetBool("no-seed"); noSeed {
		return nil
	}
	// A new database starts with the built-in lists in its shared list.
	for _, l := range seed.Lists() {
		if _, err := sh.UpdateSeed(cmd.Context(), l, false); err != nil {
			return fmt.Errorf("seed %s: %w", l.ID, err)
		}
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "Shared list seeded with %d built-in lists\n", len(seed.Lists()))
	return nil
}

func parseOptionalFloat(name, value string) (*float64, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil {
		return nil, fmt.Errorf("parse --%s: %w", name, err)
	}
	return &v, nil
}

func splitTags(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return strings.Split(value, ",")
}

func ctx(cmd *cobra.Command) context.Context { return cmd.Context() }
