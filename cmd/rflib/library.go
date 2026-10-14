package main

import (
	"fmt"

	"github.com/ryan/rflib/internal/app"
	"github.com/ryan/rflib/internal/domain"
	"github.com/ryan/rflib/internal/store"
	"github.com/spf13/cobra"
)

func newLibraryCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "library", Short: "Manage separate libraries in one database",
		Long: `A library is a separate collection of groups, channels, radios, and saved
searches; names only have to be unique within one. Every command works on
one library: --library NAME, else $RFLIB_LIBRARY, else "default", which every
database starts with.

To copy channels between libraries, export from one and import into the
other:

  rflib export json --library home | rflib import json --library club`}
	cmd.AddCommand(newLibraryAddCommand(), newLibraryListCommand(), newLibraryUpdateCommand(), newLibraryDeleteCommand())
	return cmd
}

func newLibraryAddCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "add NAME", Short: "Create a library", Args: cobra.ExactArgs(1)}
	cmd.Flags().String("description", "", "Description")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return withDB(cmd, func(d store.Store) error {
			lib, err := d.CreateLibrary(ctx(cmd), domain.Library{Name: args[0], Description: getString(cmd, "description")})
			if err == nil {
				fmt.Fprintln(cmd.OutOrStdout(), lib.ID)
			}
			return err
		})
	}
	return cmd
}

func newLibraryListCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "list", Short: "List libraries; * marks the one in use", Args: cobra.NoArgs}
	formatFlag(cmd)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		format, err := getFormat(cmd)
		if err != nil {
			return err
		}
		return withDB(cmd, func(d store.Store) error {
			libs, err := d.ListLibraries(ctx(cmd))
			if err != nil {
				return err
			}
			type row struct {
				ID          string `json:"id"`
				Name        string `json:"name"`
				Description string `json:"description"`
				Channels    int    `json:"channels"`
				Radios      int    `json:"radios"`
				InUse       bool   `json:"in_use"`
			}
			current := libraryName(cmd)
			rows := make([]row, len(libs))
			for i, lib := range libs {
				a := app.New(d.Library(lib.ID))
				cs, err := a.ListChannels(ctx(cmd))
				if err != nil {
					return err
				}
				rs, err := a.ListRadios(ctx(cmd))
				if err != nil {
					return err
				}
				rows[i] = row{lib.ID, lib.Name, lib.Description, len(cs), len(rs), lib.Name == current || lib.ID == current}
			}
			if format == "json" {
				return writeJSON(cmd.OutOrStdout(), rows)
			}
			for _, r := range rows {
				fmt.Fprintf(cmd.OutOrStdout(), "%s%s\t%d channels\t%d radios\t%s\t%s\n", mark(r.InUse, "*"), r.Name, r.Channels, r.Radios, r.ID, r.Description)
			}
			return nil
		})
	}
	return cmd
}

func newLibraryUpdateCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "update LIBRARY", Short: "Rename or describe a library; only supplied flags change", Args: cobra.ExactArgs(1)}
	cmd.Flags().String("name", "", "New name")
	cmd.Flags().String("description", "", "Description")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return withDB(cmd, func(d store.Store) error {
			lib, err := d.GetLibrary(ctx(cmd), args[0])
			if err != nil {
				return err
			}
			if cmd.Flags().Changed("name") {
				lib.Name = getString(cmd, "name")
			}
			if cmd.Flags().Changed("description") {
				lib.Description = getString(cmd, "description")
			}
			_, err = d.UpdateLibrary(ctx(cmd), lib)
			return err
		})
	}
	return cmd
}

func newLibraryDeleteCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "delete LIBRARY", Short: "Delete a library and everything in it", Args: cobra.ExactArgs(1)}
	cmd.Flags().Bool("yes", false, "Confirm deletion")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := requireYes(cmd, "library"); err != nil {
			return err
		}
		return withDB(cmd, func(d store.Store) error {
			lib, err := d.GetLibrary(ctx(cmd), args[0])
			if err != nil {
				return err
			}
			return d.DeleteLibrary(ctx(cmd), lib.ID)
		})
	}
	return cmd
}
