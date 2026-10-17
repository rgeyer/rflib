package main

import (
	"errors"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/ryan/rflib/internal/app"
	"github.com/ryan/rflib/internal/domain"
	"github.com/ryan/rflib/internal/program"
)

// editing resolves a radio to its open draft, for a command that changes
// its list; without one, it says how to start one.
func editing(cmd *cobra.Command, a *app.Service, ref string) (string, error) {
	id, err := a.EditDraft(ctx(cmd), ref)
	if errors.Is(err, app.ErrNoDraft) {
		return "", fmt.Errorf("%s has no draft; changes to its list go to one: start it with rflib radio draft %s --start", ref, quoteArg(ref))
	}
	return id, err
}

// viewing resolves a radio for a command that reads its list: its draft
// while one is open (saying so on stderr), unless --saved asks for the
// saved radio.
func viewing(cmd *cobra.Command, a *app.Service, ref string) (string, error) {
	if f := cmd.Flags().Lookup("saved"); f != nil && f.Value.String() == "true" {
		r, err := a.SavedRadio(ctx(cmd), ref)
		return r.ID, err
	}
	id, draft, err := a.ViewDraft(ctx(cmd), ref)
	if err == nil && draft {
		fmt.Fprintf(cmd.ErrOrStderr(), "showing %s's draft; --saved shows the saved radio\n", ref)
	}
	return id, err
}

// savedFlag adds --saved to a command that reads a radio's list.
func savedFlag(cmd *cobra.Command) {
	cmd.Flags().Bool("saved", false, "Show the saved radio, not its open draft")
}

// noteDraft says on stderr that a radio has an open draft that a command
// leaves out, such as an export of the saved radio.
func noteDraft(cmd *cobra.Command, a *app.Service, ref, what string) {
	if _, d, err := a.Draft(ctx(cmd), ref); err == nil && d != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "%s has an unsaved draft (started %s); %s\n", d.Name, d.CreatedAt.Local().Format("2006-01-02 15:04"), what)
	}
}

func quoteArg(s string) string {
	if strings.ContainsAny(s, " \t'\"") {
		return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
	}
	return s
}

func newRadioDraftCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "draft RADIO", Short: "Hold changes to a radio's list in a draft until you save them", Args: cobra.ExactArgs(1),
		Long: `Changes to a radio's list (its channels, sections, and sources, including
sync) go to its draft, which you save or discard when done; nothing changes
on the radio until then. A radio has one draft at most, which the web and the
CLI share. Settings (rflib radio update) save at once.

  --start     open a draft, a copy of the radio's list as it is now
  --save      make the radio's list the draft's, and close it
  --discard   close the draft, leaving the radio as it was

With none of them, it says whether a draft is open, since when, and what it
changes: memories added, removed, moved to another section, or with other
settings, how many are renumbered, and sections and sources added or removed. While a draft
is open, commands that list the radio's channels, sections, and sources show
the draft (--saved shows the radio), and exports use the saved radio.`}
	cmd.Flags().Bool("start", false, "Open a draft")
	cmd.Flags().Bool("save", false, "Save the draft")
	cmd.Flags().Bool("discard", false, "Discard the draft")
	cmd.MarkFlagsMutuallyExclusive("start", "save", "discard")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		start, _ := cmd.Flags().GetBool("start")
		save, _ := cmd.Flags().GetBool("save")
		discard, _ := cmd.Flags().GetBool("discard")
		status := cmd.ErrOrStderr()
		return withApp(cmd, func(a *app.Service) error {
			switch {
			case start:
				d, err := a.StartDraft(ctx(cmd), args[0])
				if err == nil {
					fmt.Fprintf(status, "Started a draft of %s; save it with rflib radio draft %s --save\n", d.Name, quoteArg(d.Name))
				}
				return err
			case save:
				r, err := a.SaveDraft(ctx(cmd), args[0])
				if err == nil {
					fmt.Fprintf(status, "Saved %s's draft; program the radio again\n", r.Name)
				}
				return draftError(err, args[0])
			case discard:
				r, err := a.DiscardDraft(ctx(cmd), args[0])
				if err == nil {
					fmt.Fprintf(status, "Discarded %s's draft\n", r.Name)
				}
				return draftError(err, args[0])
			}
			r, d, ch, err := a.CompareDraft(ctx(cmd), args[0])
			if err != nil {
				return err
			}
			if d == nil {
				fmt.Fprintf(status, "%s has no draft\n", r.Name)
				return nil
			}
			fmt.Fprintf(status, "%s has a draft, started %s\n", r.Name, d.CreatedAt.Local().Format("2006-01-02 15:04"))
			if ch.Empty() {
				fmt.Fprintln(status, "It changes nothing yet")
				return nil
			}
			return writeDraftChanges(cmd, ch)
		})
	}
	return cmd
}

func draftError(err error, ref string) error {
	if errors.Is(err, app.ErrNoDraft) {
		return fmt.Errorf("%s has no draft", ref)
	}
	return err
}

// writeDraftChanges lists what a draft changes, one line each.
func writeDraftChanges(cmd *cobra.Command, ch app.DraftChanges) error {
	tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	number := func(n int) string {
		if n == program.NoNumber {
			return "-"
		}
		return fmt.Sprint(n)
	}
	mhz := func(c domain.Channel) string { return domain.FormatFrequencyMHz(c.ReceiveHz) }
	for _, c := range ch.Added {
		fmt.Fprintf(tw, "added\t%s\t%s\t\n", c.Name, mhz(c))
	}
	for _, c := range ch.Removed {
		fmt.Fprintf(tw, "removed\t%s\t%s\t\n", c.Name, mhz(c))
	}
	for _, m := range ch.Moved {
		where := number(m.From) + " -> " + number(m.To)
		if m.FromSec != m.ToSec {
			where += fmt.Sprintf(" (%s -> %s)", m.FromSec, m.ToSec)
		}
		fmt.Fprintf(tw, "moved\t%s\t%s\t%s\n", m.Channel.Name, mhz(m.Channel), where)
	}
	if n := len(ch.Renumbered); n > 0 {
		fmt.Fprintf(tw, "renumbered\t%d memories\t\t\n", n)
	}
	for _, c := range ch.Changed {
		fmt.Fprintf(tw, "changed\t%s\t%s\t%s\n", c.Channel.Name, mhz(c.Channel), strings.Join(c.Changes, "; "))
	}
	for _, s := range ch.SectionsAdded {
		fmt.Fprintf(tw, "section added\t%s\t\t\n", s)
	}
	for _, s := range ch.SectionsRemoved {
		fmt.Fprintf(tw, "section removed\t%s\t\t\n", s)
	}
	for _, s := range ch.SourcesAdded {
		fmt.Fprintf(tw, "source added\t%s\t\t\n", s)
	}
	for _, s := range ch.SourcesRemoved {
		fmt.Fprintf(tw, "source removed\t%s\t\t\n", s)
	}
	return tw.Flush()
}
