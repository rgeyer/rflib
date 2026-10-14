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
	"github.com/ryan/rflib/internal/store"
	"github.com/ryan/rflib/internal/store/sqlite"
	"github.com/spf13/cobra"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string, out, errOut io.Writer) error {
	cmd := newRootCmd(out, errOut)
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

// libraryName resolves the library: --library, then $RFLIB_LIBRARY, then
// the default library.
func libraryName(cmd *cobra.Command) string {
	if v, _ := cmd.Flags().GetString("library"); v != "" {
		return v
	}
	if v := os.Getenv("RFLIB_LIBRARY"); v != "" {
		return v
	}
	return domain.DefaultLibrary
}

// withApp opens the database and the chosen library's services.
func withApp(cmd *cobra.Command, fn func(*app.Service) error) error {
	return withDB(cmd, func(d store.Store) error {
		a, err := app.Open(cmd.Context(), d, libraryName(cmd))
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
	d, err := sqlite.Open(path)
	if err != nil {
		return err
	}
	defer d.Close()
	if err = d.Migrate(cmd.Context()); err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), "Library initialized:", path)
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
