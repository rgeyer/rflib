package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ryan/rflib/internal/app"
	"github.com/ryan/rflib/internal/domain"
	"github.com/ryan/rflib/internal/program"
)

func newRadioSectionsCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "sections", Short: "Arrange a radio's list in sections",
		Long: `A radio's list is in sections, in order; every memory belongs to one. A
section starts at --start, or right after the section before it. --size
reserves that many memories, so the next section without a start of its own
begins after them; a section holding more spills over, with a warning. A
section's memories are in the order placed (manual), or sorted by name or freq.

Sources feed sections (rflib radio sources add --section), and channels added
by hand go in the default section unless --section says otherwise. Sections
are named by their names, ignoring case.`}
	cmd.AddCommand(newRadioSectionsListCommand(), newRadioSectionsAddCommand(), newRadioSectionsSetCommand(),
		newRadioSectionsMoveCommand(), newRadioSectionsRemoveCommand())
	return cmd
}

type sectionRow struct {
	program.SectionRange
	channels int
}

var sectionListing = listing[sectionRow]{
	fields: []field[sectionRow]{
		{name: "number", text: func(r sectionRow) string { return strconv.Itoa(r.Section.Position + 1) }, json: func(r sectionRow) any { return r.Section.Position + 1 }},
		{name: "name", text: func(r sectionRow) string { return r.Section.Name + mark(r.Section.Default, "*") }, json: func(r sectionRow) any { return r.Section.Name }},
		{name: "start", text: func(r sectionRow) string { return optionalNumber(r.Section.Start) }, json: func(r sectionRow) any { return r.Section.Start }},
		{name: "size", text: func(r sectionRow) string { return optionalNumber(r.Section.Size) }, json: func(r sectionRow) any { return r.Section.Size }},
		{name: "memories", text: func(r sectionRow) string {
			switch {
			case r.Section.Size != nil:
				return fmt.Sprintf("%d-%d", r.First, r.First+*r.Section.Size-1)
			case r.Last < r.First:
				return ""
			}
			return fmt.Sprintf("%d-%d", r.First, r.Last)
		}},
		{name: "first", text: func(r sectionRow) string { return strconv.Itoa(r.First) }, json: func(r sectionRow) any { return r.First }},
		{name: "last", text: func(r sectionRow) string { return strconv.Itoa(r.Last) }, json: func(r sectionRow) any { return r.Last }},
		{name: "sort", text: func(r sectionRow) string { return orDefault(r.Section.Sort, "manual") }},
		{name: "default", text: func(r sectionRow) string { return yesNo(r.Section.Default) }, json: func(r sectionRow) any { return r.Section.Default }},
		{name: "channels", text: func(r sectionRow) string { return strconv.Itoa(r.channels) }, json: func(r sectionRow) any { return r.channels }},
		{name: "id", id: true, text: func(r sectionRow) string { return r.Section.ID }},
	},
	defaults: []string{"number", "name", "start", "size", "memories", "sort", "channels"},
}

func optionalNumber(n *int) string {
	if n == nil {
		return ""
	}
	return strconv.Itoa(*n)
}

func newRadioSectionsListCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "list RADIO", Short: "List a radio's sections and the memories each uses", Args: cobra.ExactArgs(1),
		Long: "List a radio's sections in order: start, size, the memories each uses (or reserves), order, and how many channels each holds. * marks the default section."}
	sectionListing.flags(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := sectionListing.check(cmd); err != nil {
			return err
		}
		return withApp(cmd, func(a *app.Service) error {
			p, es, err := a.Layout(ctx(cmd), args[0])
			if err != nil {
				return err
			}
			count := map[string]int{}
			for _, e := range es {
				count[e.Section.ID]++
			}
			rows := make([]sectionRow, len(p.Sections))
			for i, x := range p.Sections {
				rows[i] = sectionRow{x, count[x.Section.ID]}
			}
			return sectionListing.write(cmd, rows)
		})
	}
	return cmd
}

// sectionFlags are the flags that shape a section.
func sectionFlags(cmd *cobra.Command, update bool) {
	clears := ""
	if update {
		clears = " (empty takes it away)"
	}
	cmd.Flags().String("start", "", "First memory number"+clears+"; without one, it follows the section before it")
	cmd.Flags().String("size", "", "Memories it reserves"+clears)
	cmd.Flags().String("sort", "", "Order of its memories: manual, name, or freq")
	cmd.Flags().Bool("default", false, "Make it the default section, where channels added by hand go")
}

// sectionEdit reads the flags a command was given.
func sectionEdit(cmd *cobra.Command) (app.SectionEdit, error) {
	var e app.SectionEdit
	for _, f := range []struct {
		name  string
		dst   **int
		clear *bool
	}{{"start", &e.Start, &e.ClearStart}, {"size", &e.Size, &e.ClearSize}} {
		if !cmd.Flags().Changed(f.name) {
			continue
		}
		v := strings.TrimSpace(getString(cmd, f.name))
		if v == "" {
			*f.clear = true
			continue
		}
		n, err := strconv.Atoi(v)
		if err != nil {
			return e, fmt.Errorf("--%s must be a number", f.name)
		}
		*f.dst = &n
	}
	if cmd.Flags().Changed("sort") {
		v := getString(cmd, "sort")
		e.Sort = &v
	}
	e.Default, _ = cmd.Flags().GetBool("default")
	return e, nil
}

func newRadioSectionsAddCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "add RADIO NAME", Short: "Add a section, before the default section when that is last", Args: cobra.ExactArgs(2)}
	sectionFlags(cmd, false)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		e, err := sectionEdit(cmd)
		if err != nil {
			return err
		}
		sec := domain.RadioSection{Name: args[1], Start: e.Start, Size: e.Size, Default: e.Default}
		if e.Sort != nil && *e.Sort != "manual" {
			sec.Sort = *e.Sort
		}
		return withApp(cmd, func(a *app.Service) error {
			sec, err := a.AddSection(ctx(cmd), args[0], sec)
			if err == nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "Added section %d, %s\n", sec.Position+1, sec.Name)
			}
			return err
		})
	}
	return cmd
}

func newRadioSectionsSetCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "set RADIO SECTION", Short: "Change a section; only supplied flags change", Args: cobra.ExactArgs(2)}
	cmd.Flags().String("name", "", "New name")
	sectionFlags(cmd, true)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		e, err := sectionEdit(cmd)
		if err != nil {
			return err
		}
		if cmd.Flags().Changed("name") {
			name := getString(cmd, "name")
			e.Name = &name
		}
		if e == (app.SectionEdit{}) {
			return fmt.Errorf("nothing to change; give --name, --start, --size, --sort, or --default")
		}
		return withApp(cmd, func(a *app.Service) error {
			_, err := a.UpdateSection(ctx(cmd), args[0], args[1], e)
			return err
		})
	}
	return cmd
}

func newRadioSectionsMoveCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "move RADIO SECTION", Short: "Move a section among the radio's sections", Args: cobra.ExactArgs(2)}
	cmd.Flags().Int("to", 0, "New number for the section, from 1 (required)")
	_ = cmd.MarkFlagRequired("to")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		to, _ := cmd.Flags().GetInt("to")
		return withApp(cmd, func(a *app.Service) error { return a.MoveSection(ctx(cmd), args[0], args[1], to) })
	}
	return cmd
}

func newRadioSectionsRemoveCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "remove RADIO SECTION", Short: "Remove a section; its channels and sources go to the default section", Args: cobra.ExactArgs(2)}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return withApp(cmd, func(a *app.Service) error {
			if err := a.RemoveSection(ctx(cmd), args[0], args[1]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "Removed section %s; its channels are at the end of the default section\n", args[1])
			return nil
		})
	}
	return cmd
}
