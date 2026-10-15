package main

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/ryan/rflib/internal/app"
	"github.com/ryan/rflib/internal/domain"
	"github.com/ryan/rflib/internal/importer"
	"github.com/ryan/rflib/internal/store"
	"github.com/spf13/cobra"
)

const sharedHelp = `The shared library holds channels shared with everyone, such as the built-in
lists and, later, channels from the hosted service. Your library links to
them: a link shows the shared channel and follows its corrections, and keeps
your own tags, notes, skip, power, and short names. Find shared channels with
in:shared (rflib channel list in:shared band:2m), link them with rflib
channel link, and make a private copy you can edit with rflib channel copy.

A shared channel has a status: active, off-air (temporarily, with an
expected-back date if known), or retired (possibly replaced by another).
Off-air channels stay on your radios but aren't scanned. rflib shared
changes lists links whose shared channel changed, with the radios they're on.`

// withShared opens the shared library's operations.
func withShared(cmd *cobra.Command, fn func(*app.Service) error) error {
	return withDB(cmd, func(d store.Store) error {
		a, err := app.Open(cmd.Context(), d, domain.SharedLibrary)
		if err != nil {
			return err
		}
		return fn(a)
	})
}

func newSharedCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "shared", Short: "Link to shared channels and follow their changes", Long: sharedHelp}
	cmd.AddCommand(newSharedLinkCommand(), newSharedChangesCommand(), newSharedAckCommand(), newSharedRelinkCommand(), newSharedHistoryCommand(),
		newSharedImportCommand(), newSharedUpdateCommand(), newSharedStatusCommand())
	return cmd
}

// --- in a library ---

func newChannelLinkCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "link", Short: "Link shared channels into the library", Args: cobra.NoArgs,
		Long: `Link shared channels into the library, chosen by --id or --query from the shared
list. A link shows the shared channel and follows its changes; your own tags,
notes, skip, power, and short names stay yours. The new links' IDs go to
stdout.

  rflib channel list in:shared band:noaa
  rflib channel link -q 'band:noaa' --tags weather

` + stdinIDHelp}
	idsFlag(cmd, "Shared channel IDs")
	cmd.Flags().StringP("query", "q", "", "Link the shared channels matching this query")
	cmd.Flags().String("tags", "", "Comma-separated tags of your own for the new links")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		sel, err := selection(cmd)
		if err != nil {
			return err
		}
		return withApp(cmd, func(a *app.Service) error {
			res, err := a.LinkChannels(ctx(cmd), sel, splitTags(getString(cmd, "tags")))
			if err != nil {
				return err
			}
			for _, c := range res.Linked {
				fmt.Fprintln(cmd.OutOrStdout(), c.ID)
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "Linked %d channels", len(res.Linked))
			if n := len(res.Already); n > 0 {
				fmt.Fprintf(cmd.ErrOrStderr(), "; %d already linked", n)
			}
			fmt.Fprintln(cmd.ErrOrStderr())
			return nil
		})
	}
	return cmd
}

func newChannelCopyCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "copy", Short: "Make links into private channels you can edit", Args: cobra.NoArgs,
		Long: `Make links into private channels showing what they showed, with the shared
tags and notes made your own. They keep their IDs, so radios keep them, but
stop following the shared channel's changes. Private channels in the selection
are left alone.

` + stdinIDHelp}
	idsFlag(cmd, "Channel IDs")
	cmd.Flags().StringP("query", "q", "", "Copy the links matching this query")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		sel, err := selection(cmd)
		if err != nil {
			return err
		}
		return withApp(cmd, func(a *app.Service) error {
			cs, err := a.CopyChannels(ctx(cmd), sel)
			if err == nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "Copied %d links to private channels\n", len(cs))
			}
			return err
		})
	}
	return cmd
}

func newSharedLinkCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "link", Short: "Turn private channels that match shared ones into links", Args: cobra.NoArgs,
		Long: `Turn the library's private channels into links to the shared channels they
match on the air: the same frequencies, tones, and tone squelch. Each keeps
its ID, so radios keep it, and its own tags and notes; a name that differs
from the shared one is kept as a short name. Lists the changes on stdout.`}
	cmd.Flags().Bool("dry-run", false, "List what would become a link without changing anything")
	conversionListing.flags(cmd)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if err := conversionListing.check(cmd); err != nil {
			return err
		}
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		return withApp(cmd, func(a *app.Service) error {
			cvs, err := a.ConvertToLinks(ctx(cmd), nil, nil, dryRun)
			if err != nil {
				return err
			}
			if err := conversionListing.write(cmd, cvs); err != nil {
				return err
			}
			verb := "Linked"
			if dryRun {
				verb = "Would link"
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "%s %d channels\n", verb, len(cvs))
			return nil
		})
	}
	return cmd
}

var conversionListing = listing[app.Conversion]{
	fields: []field[app.Conversion]{
		{name: "id", id: true, text: func(c app.Conversion) string { return c.Private.ID }},
		{name: "name", text: func(c app.Conversion) string { return c.Private.Name }},
		{name: "shared", text: func(c app.Conversion) string { return c.Shared.Name }},
		{name: "rx", text: func(c app.Conversion) string { return domain.FormatFrequencyMHz(c.Shared.ReceiveHz) }},
		{name: "shared_id", id: true, text: func(c app.Conversion) string { return c.Shared.ID }},
	},
	defaults: []string{"id", "name", "shared", "rx"},
}

func newSharedChangesCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "changes", Short: "List links whose shared channel changed", Args: cobra.NoArgs,
		Long: `List the library's links whose shared channel changed since you acknowledged
it: what changed, why, and the radios it's on, which need programming again.
A retired channel shows its replacement; rflib shared relink moves links over
to it. rflib shared ack marks changes as seen.`}
	changeListing.flags(cmd)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if err := changeListing.check(cmd); err != nil {
			return err
		}
		return withApp(cmd, func(a *app.Service) error {
			chs, err := a.Changes(ctx(cmd))
			if err != nil {
				return err
			}
			return changeListing.write(cmd, chs)
		})
	}
	return cmd
}

var changeListing = listing[app.Change]{
	fields: []field[app.Change]{
		{name: "id", id: true, text: func(c app.Change) string { return c.Channel.ID }},
		{name: "name", text: func(c app.Change) string { return c.Channel.Name }},
		{name: "status", text: func(c app.Change) string { return string(c.Channel.Status.OrActive()) }},
		{name: "changes", text: func(c app.Change) string { return strings.Join(c.Changes, "; ") }, json: func(c app.Change) any { return nonNil(c.Changes) }},
		{name: "reasons", text: func(c app.Change) string { return strings.Join(c.Reasons, "; ") }, json: func(c app.Change) any { return nonNil(c.Reasons) }},
		{name: "radios", text: func(c app.Change) string { return strings.Join(c.Radios, ",") }, json: func(c app.Change) any { return nonNil(c.Radios) }},
		{name: "replacement", text: func(c app.Change) string {
			if c.Replacement == nil {
				return ""
			}
			return c.Replacement.Name + " (" + c.Replacement.ID + ")"
		}},
	},
	defaults: []string{"id", "name", "status", "changes", "reasons", "radios", "replacement"},
}

func linkSelection(cmd *cobra.Command) {
	idsFlag(cmd, "Channel IDs")
	cmd.Flags().StringP("query", "q", "", "The links matching this query")
	cmd.Flags().Bool("all", false, "Every link with changes")
}

func newSharedAckCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "ack", Short: "Mark links' shared changes as seen", Args: cobra.NoArgs}
	linkSelection(cmd)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		sel, err := selection(cmd)
		if err != nil {
			return err
		}
		return withApp(cmd, func(a *app.Service) error {
			cs, err := a.Acknowledge(ctx(cmd), sel)
			if err == nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "Acknowledged changes to %d links\n", len(cs))
			}
			return err
		})
	}
	return cmd
}

func newSharedRelinkCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "relink", Short: "Move links to retired channels over to their replacements", Args: cobra.NoArgs,
		Long: `Move links to retired shared channels over to the channels that replaced them.
The links keep their IDs, so radios keep them; program the radios again.`}
	linkSelection(cmd)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		sel, err := selection(cmd)
		if err != nil {
			return err
		}
		return withApp(cmd, func(a *app.Service) error {
			cs, err := a.Relink(ctx(cmd), sel)
			if err == nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "Relinked %d channels to their replacements\n", len(cs))
			}
			return err
		})
	}
	return cmd
}

func newSharedHistoryCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "history", Short: "Show a shared channel's revisions", Args: cobra.NoArgs,
		Long: "Show a shared channel's revisions, oldest first: when, why, and what changed.\nGive a link's ID to see its shared channel's."}
	idFlag(cmd, "Channel ID, or a link's (required)")
	revisionListing.flags(cmd)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if err := revisionListing.check(cmd); err != nil {
			return err
		}
		id, err := getID(cmd)
		if err != nil {
			return err
		}
		show := func(a *app.Service) error {
			_, revs, err := a.History(ctx(cmd), id)
			if err != nil {
				return err
			}
			rows := make([]revisionRow, len(revs))
			for i, r := range revs {
				rows[i].Revision = r
				if i > 0 {
					rows[i].changes = domain.SharedChanges(revs[i-1].Channel, r.Channel)
				}
			}
			return revisionListing.write(cmd, rows)
		}
		// A shared channel's ID works from any library.
		err = withApp(cmd, show)
		if errors.Is(err, store.ErrNotFound) {
			if shErr := withShared(cmd, show); shErr == nil {
				return nil
			}
		}
		return err
	}
	return cmd
}

type revisionRow struct {
	domain.Revision
	changes []string
}

var revisionListing = listing[revisionRow]{
	fields: []field[revisionRow]{
		{name: "revision", text: func(r revisionRow) string { return fmt.Sprint(r.Number) }, json: func(r revisionRow) any { return r.Number }},
		{name: "at", text: func(r revisionRow) string { return r.At.Local().Format("2006-01-02 15:04") }, json: func(r revisionRow) any { return r.At }},
		{name: "reason", text: func(r revisionRow) string { return r.Reason }},
		{name: "author", text: func(r revisionRow) string { return r.Author }},
		{name: "changes", text: func(r revisionRow) string {
			if r.Number == 1 {
				return "listed"
			}
			return strings.Join(r.changes, "; ")
		}, json: func(r revisionRow) any { return nonNil(r.changes) }},
	},
	defaults: []string{"revision", "at", "reason", "changes"},
}

// --- maintainer commands on the shared library ---

func reasonFlag(cmd *cobra.Command) {
	cmd.Flags().String("reason", "", "Why, recorded in each changed channel's history (required)")
	_ = cmd.MarkFlagRequired("reason")
}

func newSharedImportCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "import csv|json|chirp FILE", Short: "Add or correct shared channels from a file (maintainer)", Args: cobra.RangeArgs(1, 2),
		Long: `Add channels to the shared library from a file, or correct them with
--on-duplicate update. Every change is recorded in the channel's history with
--reason. Tags that aren't shared tags (rflib tag list) are left off.`}
	importFlags(cmd)
	cmd.Flags().Bool("skip-invalid", false, "Import valid rows even when some rows are invalid")
	reasonFlag(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		parsers := map[string]func(io.Reader) (importer.Batch, error){"csv": importer.ParseGenericCSV, "json": importer.ParseJSON, "chirp": importer.ParseCHIRP}
		parse, ok := parsers[args[0]]
		if !ok {
			return fmt.Errorf("unknown format %q; use csv, json, or chirp", args[0])
		}
		opts, err := importOptions(cmd)
		if err != nil {
			return err
		}
		b, err := parseInput(cmd, args[1:], parse)
		if err != nil {
			return err
		}
		invalid, err := checkRowErrors(cmd, b)
		if err != nil {
			return err
		}
		return withShared(cmd, func(a *app.Service) error {
			sum, dropped, err := a.ImportShared(ctx(cmd), b, opts, getString(cmd, "reason"))
			if err != nil {
				return err
			}
			if len(dropped) > 0 {
				fmt.Fprintf(cmd.ErrOrStderr(), "Left off tags that aren't shared tags: %s\n", strings.Join(dropped, ", "))
			}
			printSummary(cmd.ErrOrStderr(), sum, invalid, opts.DryRun)
			return nil
		})
	}
	return cmd
}

func newSharedUpdateCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "update", Short: "Correct a shared channel (maintainer)", Args: cobra.NoArgs,
		Long: "Correct a shared channel; only supplied flags change. Links follow the change,\nand it's recorded in the channel's history with --reason."}
	idFlag(cmd, "Shared channel ID (required)")
	channelFields(cmd)
	orgFlags(cmd)
	reasonFlag(cmd)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		return withShared(cmd, func(a *app.Service) error {
			c, err := a.GetChannel(ctx(cmd), getString(cmd, "id"))
			if err != nil {
				return err
			}
			if err := applyChannelFlags(cmd, a, &c, false); err != nil {
				return err
			}
			if c.Orgs, err = applyOrgFlags(cmd, a, c.Orgs); err != nil {
				return err
			}
			c, warnings, err := a.UpdateShared(ctx(cmd), c, getString(cmd, "reason"))
			if err == nil {
				warnBandPlan(cmd, c, warnings)
				fmt.Fprintf(cmd.ErrOrStderr(), "%s is at revision %d\n", c.Name, c.Revision)
			}
			return err
		})
	}
	return cmd
}

func newSharedStatusCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "status", Short: "Set whether a shared channel is on the air (maintainer)", Args: cobra.NoArgs,
		Long: `Set a shared channel's status: active, off-air (temporarily; --back gives the
expected date), or retired (gone for good; --replaced-by names the channel
that took its place). Links follow, and the change is recorded with --reason.`}
	idFlag(cmd, "Shared channel ID (required)")
	cmd.Flags().String("set", "", "active, off-air, or retired (required)")
	_ = cmd.MarkFlagRequired("set")
	cmd.Flags().String("note", "", "What's happening, such as \"antenna work\"")
	cmd.Flags().String("back", "", "For off-air: when it's expected back, YYYY-MM-DD")
	cmd.Flags().String("replaced-by", "", "For retired: the shared channel ID that replaces it")
	reasonFlag(cmd)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		st, err := domain.ParseStatus(getString(cmd, "set"))
		if err != nil {
			return err
		}
		ch := app.StatusChange{Status: st, Note: getString(cmd, "note"), ReplacedBy: getString(cmd, "replaced-by")}
		if v := getString(cmd, "back"); v != "" {
			t, err := time.Parse(time.DateOnly, v)
			if err != nil {
				return fmt.Errorf("--back must be a date, YYYY-MM-DD")
			}
			ch.BackOn = &t
		}
		return withShared(cmd, func(a *app.Service) error {
			c, err := a.SetStatus(ctx(cmd), getString(cmd, "id"), ch, getString(cmd, "reason"))
			if err == nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s is %s, at revision %d\n", c.Name, c.Status.OrActive(), c.Revision)
			}
			return err
		})
	}
	return cmd
}

// --- tags ---

func newTagCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "tag", Short: "List the shared tags and what they mean"}
	list := &cobra.Command{Use: "list", Short: "List the shared tags and what they mean", Args: cobra.NoArgs,
		Long: "List the tags shared channels may carry, and what each means. Your own tags\nare free-form; one with a shared tag's name means the same thing."}
	tagListing.flags(list)
	list.RunE = func(cmd *cobra.Command, _ []string) error {
		return tagListing.write(cmd, domain.CuratedTags)
	}
	cmd.AddCommand(list)
	return cmd
}

var tagListing = listing[struct{ Name, Meaning string }]{
	fields: []field[struct{ Name, Meaning string }]{
		{name: "name", text: func(t struct{ Name, Meaning string }) string { return t.Name }},
		{name: "meaning", text: func(t struct{ Name, Meaning string }) string { return t.Meaning }},
	},
	defaults: []string{"name", "meaning"},
}
