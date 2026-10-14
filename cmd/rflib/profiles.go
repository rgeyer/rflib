package main

import (
	"fmt"
	"os"

	"github.com/ryan/rflib/internal/profile"
	"github.com/spf13/cobra"
)

var profileListing = listing[profile.Entry]{
	fields: []field[profile.Entry]{
		{name: "name", text: func(e profile.Entry) string { return e.Profile.Name }},
		{name: "description", text: func(e profile.Entry) string { return e.Profile.Description }},
		{name: "origin", text: func(e profile.Entry) string { return e.Origin }},
	},
	defaults: []string{"name", "description", "origin"},
}

func newProfileCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "profile", Short: "List and check export profiles",
		Long: `Export profiles describe CSV formats for radio programming software. Built-in
profiles ship with rflib; add your own as .yaml files in the directory shown by
"rflib profile list". A file there with a built-in's name replaces it.`}

	list := &cobra.Command{Use: "list", Short: "List available profiles", Args: cobra.NoArgs}
	profileListing.flags(list)
	list.RunE = func(cmd *cobra.Command, _ []string) error {
		entries, err := profile.Available()
		if err != nil {
			return err
		}
		if err := profileListing.write(cmd, entries); err != nil {
			return err
		}
		if dir, err := profile.UserDir(); err == nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "User profiles are read from %s\n", dir)
		}
		return nil
	}

	show := &cobra.Command{Use: "show PROFILE", Short: "Print a profile's definition, to read or copy as a starting point", Args: cobra.ExactArgs(1)}
	show.RunE = func(cmd *cobra.Command, args []string) error {
		e, err := profile.Find(args[0])
		if err != nil {
			return err
		}
		var data []byte
		if e.Origin == "built-in" {
			data, err = profile.BuiltinYAML(e.Profile.Name)
		} else {
			data, err = os.ReadFile(e.Origin)
		}
		if err != nil {
			return err
		}
		_, err = cmd.OutOrStdout().Write(data)
		return err
	}

	validate := &cobra.Command{Use: "validate FILE", Short: "Check a profile file for errors", Args: cobra.ExactArgs(1)}
	validate.RunE = func(cmd *cobra.Command, args []string) error {
		p, err := profile.Load(args[0])
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "%s is valid (%d columns)\n", p.Name, len(p.Columns))
		return nil
	}

	cmd.AddCommand(list, show, validate)
	return cmd
}
