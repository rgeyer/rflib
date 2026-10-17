package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ryan/rflib/internal/model"
	"github.com/ryan/rflib/internal/profile"
	"github.com/ryan/rflib/internal/userfiles"
)

// Radio models and export profiles can be files of the user's own, in a
// directory rflib reads beside the built-in ones. These commands install,
// remove, and check them, the same way for both.

// installed is a user file as the commands report it.
type installed struct {
	name, path, status string
	// builtin says the file replaces a built-in one.
	builtin bool
}

// userKind is models or profiles, for the commands.
type userKind struct {
	what     string
	dir      func() (string, error)
	install  func(path string, replace bool) (installed, string, error)
	remove   func(name string) (installed, error)
	files    func() ([]installed, error)
	problems func() []userfiles.Problem
}

var userModels = userKind{
	what: "model",
	dir:  model.UserDir,
	install: func(path string, replace bool) (installed, string, error) {
		e, old, err := model.Install(path, replace)
		return modelFile(e), old, err
	},
	remove: func(name string) (installed, error) {
		e, err := model.Remove(name)
		return modelFile(e), err
	},
	files: func() ([]installed, error) {
		es, err := model.Available()
		var out []installed
		for _, e := range es {
			if e.Local() {
				out = append(out, modelFile(e))
			}
		}
		return out, err
	},
	problems: model.Problems,
}

func modelFile(e model.Entry) installed {
	return installed{e.Model.Name, e.Origin, e.Status(), e.Builtin != nil}
}

var userProfiles = userKind{
	what: "profile",
	dir:  profile.UserDir,
	install: func(path string, replace bool) (installed, string, error) {
		e, old, err := profile.Install(path, replace)
		return profileFile(e), old, err
	},
	remove: func(name string) (installed, error) {
		e, err := profile.Remove(name)
		return profileFile(e), err
	},
	files: func() ([]installed, error) {
		es, err := profile.Available()
		var out []installed
		for _, e := range es {
			if e.Local() {
				out = append(out, profileFile(e))
			}
		}
		return out, err
	},
	problems: profile.Problems,
}

func profileFile(e profile.Entry) installed {
	return installed{e.Profile.Name, e.Origin, e.Status(), e.Builtin != nil}
}

// origin is where a model or profile comes from, for listings: built-in,
// or its file, with whether it replaces a built-in.
func origin(path, status string) string {
	switch status {
	case "built-in", "local":
		return path
	}
	return path + " (" + status + ")"
}

// reportFiles says on stderr which of the user's files can't be used, and
// which replace a built-in that now says the same, so removing them lets
// later fixes through.
func (k userKind) reportFiles(cmd *cobra.Command) {
	for _, p := range k.problems() {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: skipped %s: %v\n", p.Path, p.Err)
	}
	fs, _ := k.files()
	for _, f := range fs {
		if f.status == "same as the built-in" {
			fmt.Fprintf(cmd.ErrOrStderr(), "note: %s is now the same as the built-in %s %s; rflib %s remove %s uses the built-in, and its later fixes\n", f.path, k.what, f.name, k.what, f.name)
		}
	}
}

func (k userKind) installCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "install FILE", Short: "Check a " + k.what + " file and add it to your " + k.what + "s", Args: cobra.ExactArgs(1),
		Long: `Checks a ` + k.what + ` file, such as one sent to you to try, and copies it into the
directory rflib reads your ` + k.what + `s from, named after the ` + k.what + `. One with a
built-in ` + k.what + `'s name replaces the built-in. --replace replaces a ` + k.what + ` of
yours with the same name.`}
	cmd.Flags().Bool("replace", false, "Replace an installed "+k.what+" with the same name")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		replace, _ := cmd.Flags().GetBool("replace")
		f, old, err := k.install(args[0], replace)
		if err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), f.path)
		msg := fmt.Sprintf("Installed the %s %s", k.what, f.name)
		switch {
		case old != "":
			msg += ", replacing " + old
		case f.builtin:
			msg += ", in place of the built-in one"
		}
		fmt.Fprintln(cmd.ErrOrStderr(), msg)
		if f.status == "same as the built-in" {
			fmt.Fprintf(cmd.ErrOrStderr(), "note: it's the same as the built-in %s, so it changes nothing\n", k.what)
		}
		return nil
	}
	return cmd
}

func (k userKind) removeCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "remove NAME", Short: "Remove a " + k.what + " of yours", Args: cobra.ExactArgs(1),
		Long: `Deletes the file of a ` + k.what + ` of yours. When it replaced a built-in ` + k.what + `,
the built-in one is used again.`}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		f, err := k.remove(args[0])
		if err != nil {
			return err
		}
		msg := fmt.Sprintf("Removed %s", f.path)
		if f.builtin {
			msg += fmt.Sprintf("; the built-in %s %s is used again", k.what, args[0])
		}
		fmt.Fprintln(cmd.ErrOrStderr(), msg)
		return nil
	}
	return cmd
}

// validateAll checks every file of the user's, for validate with no file.
func (k userKind) validateAll(cmd *cobra.Command) error {
	fs, err := k.files()
	if err != nil {
		return err
	}
	problems := k.problems()
	dir, _ := k.dir()
	if len(fs) == 0 && len(problems) == 0 {
		fmt.Fprintf(cmd.ErrOrStderr(), "No %ss of yours in %s\n", k.what, dir)
		return nil
	}
	for _, f := range fs {
		fmt.Fprintf(cmd.ErrOrStderr(), "%s: %s is valid (%s)\n", f.path, f.name, f.status)
	}
	k.reportFiles(cmd)
	if len(problems) > 0 {
		return fmt.Errorf("%d %s file(s) in %s can't be used", len(problems), k.what, dir)
	}
	return nil
}

// modelOrigin says, after a model's name, where a model of the user's own
// and its profile come from: " (from FILE, replaces the built-in)", or ""
// for a built-in model with a built-in profile.
func modelOrigin(name string) string {
	e, err := model.Lookup(name)
	if err != nil {
		return ""
	}
	var parts []string
	if e.Local() {
		parts = append(parts, "from "+origin(e.Origin, e.Status()))
	}
	if pn, ok := e.Model.ProfileName(); ok {
		if pe, err := profile.Find(pn); err == nil && pe.Local() {
			parts = append(parts, "its profile "+pn+" from "+origin(pe.Origin, pe.Status()))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return " (" + strings.Join(parts, "; ") + ")"
}
