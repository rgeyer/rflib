package main

import (
	"fmt"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/ryan/rflib/internal/app"
	"github.com/ryan/rflib/internal/domain"
)

func newRadioRevisionsCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "revisions", Short: "Keep, compare, and restore a radio's past versions",
		Long: `A radio revision is the saved radio as it was at one time: saved by hand
(save), or by an export when the radio differs from its last revision. A
radio that differs from its last exported revision, including channels on it
that changed since, needs programming again (rflib radio list and show say
so). Compare a revision with the radio as it is now, or restore one into the
radio's draft, to review and save.`}
	cmd.AddCommand(newRevisionsListCommand(), newRevisionsSaveCommand(), newRevisionsCompareCommand(), newRevisionsRestoreCommand())
	return cmd
}

func newRevisionsListCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "list RADIO", Short: "List a radio's revisions", Args: cobra.ExactArgs(1)}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return withApp(cmd, func(a *app.Service) error {
			_, revs, err := a.RadioRevisions(ctx(cmd), args[0])
			if err != nil {
				return err
			}
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			for _, r := range revs {
				exported := ""
				if r.Exported {
					exported = "exported"
				}
				fmt.Fprintf(tw, "%d\t%s\t%s\t%d memories\t%s\n", r.Number, r.At.Local().Format("2006-01-02 15:04"), exported, len(r.Snapshot.Memories), r.Reason)
			}
			return tw.Flush()
		})
	}
	return cmd
}

func newRevisionsSaveCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "save RADIO", Short: "Save the radio as it is now as a revision", Args: cobra.ExactArgs(1)}
	cmd.Flags().String("reason", "", "Why (required)")
	_ = cmd.MarkFlagRequired("reason")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return withApp(cmd, func(a *app.Service) error {
			noteDraft(cmd, a, args[0], "saving the saved radio")
			rev, err := a.SaveRadioRevision(ctx(cmd), args[0], getString(cmd, "reason"))
			if err == nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "Saved revision %d\n", rev.Number)
			}
			return err
		})
	}
	return cmd
}

func revisionNumber(arg string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(arg))
	if err != nil || n < 1 {
		return 0, fmt.Errorf("a revision is a number from 1, not %q", arg)
	}
	return n, nil
}

func newRevisionsCompareCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "compare RADIO N", Short: "Compare a revision with the radio as it is now", Args: cobra.ExactArgs(2),
		Long: `Compare revision N with the radio as it is now (its draft, if one is open):
memories added, removed, moved to another section, or with other settings since
then, how many are renumbered, channels whose own values changed (such as a
corrected tone), and sections and sources added or removed.`}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		n, err := revisionNumber(args[1])
		if err != nil {
			return err
		}
		return withApp(cmd, func(a *app.Service) error {
			rev, ch, err := a.CompareRevision(ctx(cmd), args[0], n)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "Revision %d (%s, %s) against now\n", rev.Number, rev.At.Local().Format("2006-01-02 15:04"), orDefault(rev.Reason, "no reason given"))
			if ch.Empty() {
				fmt.Fprintln(cmd.ErrOrStderr(), "Nothing changed since")
				return nil
			}
			if err := writeDraftChanges(cmd, ch); err != nil {
				return err
			}
			return writeChannelChanges(cmd, ch.Channels)
		})
	}
	return cmd
}

// writeChannelChanges lists channels whose own values changed, one line
// per change.
func writeChannelChanges(cmd *cobra.Command, cs []app.ChannelChange) error {
	tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	for _, c := range cs {
		for _, x := range c.Changes {
			fmt.Fprintf(tw, "channel changed\t%s\t%s\t%s\n", c.Channel.Name, domain.FormatFrequencyMHz(c.Channel.ReceiveHz), x)
		}
	}
	return tw.Flush()
}

func newRevisionsRestoreCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "restore RADIO N", Short: "Put a revision's list into the radio's draft", Args: cobra.ExactArgs(2),
		Long: `Put revision N's sections, sources, and memories into the radio's draft, to
review, then save (rflib radio draft RADIO --save) or discard. Settings save at
once, so they stay as they are; channels are as they are now, since they
belong to the library, so one retired since comes back crossed out. With a
draft open, --replace-draft replaces it.`}
	cmd.Flags().Bool("replace-draft", false, "Replace the radio's open draft")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		n, err := revisionNumber(args[1])
		if err != nil {
			return err
		}
		replace, _ := cmd.Flags().GetBool("replace-draft")
		return withApp(cmd, func(a *app.Service) error {
			d, err := a.RestoreRevision(ctx(cmd), args[0], n, replace)
			if err == nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "Put revision %d into %s's draft; review it, then rflib radio draft %s --save\n", n, d.Name, quoteArg(d.Name))
			} else if strings.Contains(err.Error(), "restoring replaces it") {
				return fmt.Errorf("%w; add --replace-draft to replace it", err)
			}
			return err
		})
	}
	return cmd
}

// revisionText and programmingText describe a radio's RevisionState.
func revisionText(st app.RevisionState) string {
	if st.Latest == 0 {
		return "none"
	}
	if st.Changed {
		return fmt.Sprintf("%d, changed since", st.Latest)
	}
	return strconv.Itoa(st.Latest)
}

func programmingText(st app.RevisionState) string {
	if st.NeedsProgramming {
		return "needs programming"
	}
	return "up to date"
}
