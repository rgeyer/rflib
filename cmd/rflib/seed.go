package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ryan/rflib/internal/app"
	"github.com/ryan/rflib/internal/domain"
	"github.com/ryan/rflib/internal/seed"
	"github.com/spf13/cobra"
)

func newSeedCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "seed", Short: "Fill and update the shared list from built-in channel lists (maintainer)",
		Long: `Built-in lists seed the shared list: a new database gets every list on
rflib init, and when a newer rflib changes a list, rflib seed list shows it
pending and rflib seed update applies it. Your own library takes channels from
the shared list (see rflib channel link).`}

	list := &cobra.Command{Use: "list", Short: "List built-in channel lists and whether the shared list is up to date", Args: cobra.NoArgs}
	seedListing.flags(list)
	list.RunE = func(cmd *cobra.Command, _ []string) error {
		return withShared(cmd, func(a *app.Service) error {
			var rows []seedRow
			for _, l := range seed.Lists() {
				b, err := l.Batch()
				if err != nil {
					return err
				}
				st, err := a.SeedStatus(ctx(cmd), l)
				if err != nil {
					return fmt.Errorf("%s: %w", l.ID, err)
				}
				rows = append(rows, seedRow{st, len(b.Records)})
			}
			return seedListing.write(cmd, rows)
		})
	}

	show := &cobra.Command{Use: "show LIST", Short: "Show the channels in a built-in list", Args: cobra.ExactArgs(1)}
	seedChannelListing.flags(show)
	show.RunE = func(cmd *cobra.Command, args []string) error {
		if err := seedChannelListing.check(cmd); err != nil {
			return err
		}
		l, err := seed.Get(args[0])
		if err != nil {
			return err
		}
		b, err := l.Batch()
		if err != nil {
			return err
		}
		cs := make([]domain.Channel, len(b.Records))
		for i, r := range b.Records {
			cs[i] = r.Channel
		}
		return seedChannelListing.write(cmd, cs)
	}

	update := &cobra.Command{Use: "update [LIST...]", Aliases: []string{"install"}, Short: "Bring the shared list up to date with built-in lists",
		Long: `Add each list's new channels to the shared list, apply what the list changed
since it was last applied, and retire channels it dropped. A field a
maintainer corrected is kept, and reported. With no lists named, every list
is updated.`}
	update.Flags().Bool("dry-run", false, "Report what would change without changing anything")
	update.RunE = func(cmd *cobra.Command, args []string) error {
		lists := seed.Lists()
		if len(args) > 0 {
			lists = make([]seed.List, len(args))
			for i, id := range args {
				var err error
				if lists[i], err = seed.Get(id); err != nil {
					return err
				}
			}
		}
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		return withShared(cmd, func(a *app.Service) error {
			return updateSeeds(cmd, a, lists, dryRun)
		})
	}

	cmd.AddCommand(list, show, update)
	return cmd
}

// updateSeeds updates the shared list from each list, reporting each
// list's changes on stderr.
func updateSeeds(cmd *cobra.Command, a *app.Service, lists []seed.List, dryRun bool) error {
	out := cmd.ErrOrStderr()
	for _, l := range lists {
		st, err := a.UpdateSeed(ctx(cmd), l, dryRun)
		if err != nil {
			return fmt.Errorf("%s: %w", l.ID, err)
		}
		if !st.Pending() {
			fmt.Fprintf(out, "%s: up to date\n", l.ID)
			continue
		}
		verb := "updated"
		if dryRun {
			verb = "would update"
		}
		fmt.Fprintf(out, "%s: %s: %s\n", l.ID, verb, seedSummary(st))
		for _, ch := range st.Changes {
			switch ch.Kind {
			case "add":
				fmt.Fprintf(out, "  add %s\n", ch.Name)
			case "drop":
				fmt.Fprintf(out, "  retire %s (no longer in the list)\n", ch.Name)
			default:
				line := "  update " + ch.Name
				if len(ch.Fields) > 0 {
					line += ": " + strings.Join(ch.Fields, ", ")
				}
				if len(ch.Kept) > 0 {
					line += "; kept a maintainer's " + strings.Join(ch.Kept, ", ")
				}
				fmt.Fprintln(out, line)
			}
		}
	}
	return nil
}

// seedSummary counts a list's pending changes, or says it's not
// installed.
func seedSummary(st app.SeedStatus) string {
	n := map[string]int{}
	for _, ch := range st.Changes {
		n[ch.Kind]++
	}
	var parts []string
	for _, k := range []struct{ kind, label string }{{"add", "to add"}, {"update", "to update"}, {"drop", "to retire"}} {
		if n[k.kind] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n[k.kind], k.label))
		}
	}
	return strings.Join(parts, ", ")
}

type seedRow struct {
	app.SeedStatus
	channels int
}

func (r seedRow) status() string {
	switch {
	case !r.Installed:
		return "not installed"
	case r.Pending():
		return seedSummary(r.SeedStatus)
	}
	return "up to date"
}

var seedListing = listing[seedRow]{
	fields: []field[seedRow]{
		{name: "name", text: func(r seedRow) string { return r.List.ID }},
		{name: "channels", text: func(r seedRow) string { return strconv.Itoa(r.channels) }, json: func(r seedRow) any { return r.channels }},
		{name: "status", text: seedRow.status},
		{name: "description", text: func(r seedRow) string { return r.List.Description }},
	},
	defaults: []string{"name", "channels", "status", "description"},
}

// seedChannelListing shows a built-in list's channels, which have no IDs
// until installed.
var seedChannelListing = listing[domain.Channel]{
	fields:   channelListing.fields,
	defaults: []string{"name", "rx", "duplex", "offset", "tone", "mode", "service", "notes"},
	full:     channelListing.full,
}

type warningRow struct {
	domain.Channel
	warning string
}

var warningListing = listing[warningRow]{
	fields: []field[warningRow]{
		{name: "id", id: true, text: func(r warningRow) string { return r.ID }},
		{name: "name", text: func(r warningRow) string { return r.Name }},
		{name: "warning", text: func(r warningRow) string { return r.warning }},
	},
	defaults: []string{"id", "name", "warning"},
}

func newCheckCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "check", Short: "Report channels that don't fit their service's US band plan", Args: cobra.NoArgs,
		Long: `Report channels that don't fit their service's US band plan: amateur
transmit frequencies outside the amateur bands, off-plan FRS, GMRS, MURS, CB,
and NOAA channels, transmitting NOAA or public safety channels, and channels
without a service that transmit outside the amateur bands and FRS, GMRS, MURS,
and CB channels. Exits non-zero when anything is reported.`}
	filterFields(cmd)
	warningListing.flags(cmd)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if err := warningListing.check(cmd); err != nil {
			return err
		}
		return withApp(cmd, func(a *app.Service) error {
			cs, err := filteredChannels(cmd, a)
			if err != nil {
				return err
			}
			var rows []warningRow
			for _, c := range cs {
				for _, w := range domain.CheckBandPlan(c) {
					rows = append(rows, warningRow{c, w})
				}
			}
			if err := warningListing.write(cmd, rows); err != nil {
				return err
			}
			if len(rows) > 0 {
				return fmt.Errorf("%d band plan warnings", len(rows))
			}
			return nil
		})
	}
	return cmd
}
