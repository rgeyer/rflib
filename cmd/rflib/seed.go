package main

import (
	"fmt"
	"strconv"

	"github.com/ryan/rflib/internal/app"
	"github.com/ryan/rflib/internal/domain"
	"github.com/ryan/rflib/internal/seed"
	"github.com/spf13/cobra"
)

func newSeedCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "seed", Short: "Install built-in reference channel lists"}

	list := &cobra.Command{Use: "list", Short: "List built-in channel lists", Args: cobra.NoArgs}
	seedListing.flags(list)
	list.RunE = func(cmd *cobra.Command, _ []string) error {
		var rows []seedRow
		for _, l := range seed.Lists() {
			b, err := l.Batch()
			if err != nil {
				return err
			}
			rows = append(rows, seedRow{l, len(b.Records)})
		}
		return seedListing.write(cmd, rows)
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

	install := &cobra.Command{Use: "install LIST...", Short: "Link built-in lists into the library", Args: cobra.MinimumNArgs(1),
		Long: `Put built-in lists in the shared library and link their channels into this
library. Channels you already have that match one on the air (the same
frequencies, tones, and tone squelch) become links, keeping your tags, notes,
and, as a short name, a name of your own. Installing again changes nothing.`}
	install.Flags().String("tags", "", "Comma-separated tags of your own for the new links")
	install.Flags().Bool("dry-run", false, "Report what would change without changing anything")
	install.RunE = func(cmd *cobra.Command, args []string) error {
		lists := make([]seed.List, len(args))
		for i, id := range args {
			var err error
			if lists[i], err = seed.Get(id); err != nil {
				return err
			}
		}
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		return withApp(cmd, func(a *app.Service) error {
			for _, l := range lists {
				res, err := a.InstallSeed(ctx(cmd), l, splitTags(getString(cmd, "tags")), dryRun)
				if err != nil {
					return fmt.Errorf("%s: %w", l.ID, err)
				}
				prefix := ""
				if dryRun {
					prefix = "would have "
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "%s: %slinked %d, converted %d of your channels, %d already linked\n",
					l.ID, prefix, len(res.Linked), len(res.Converted), len(res.Already))
			}
			return nil
		})
	}

	cmd.AddCommand(list, show, install)
	return cmd
}

type seedRow struct {
	seed.List
	channels int
}

var seedListing = listing[seedRow]{
	fields: []field[seedRow]{
		{name: "name", text: func(r seedRow) string { return r.ID }},
		{name: "channels", text: func(r seedRow) string { return strconv.Itoa(r.channels) }, json: func(r seedRow) any { return r.channels }},
		{name: "description", text: func(r seedRow) string { return r.Description }},
	},
	defaults: []string{"name", "channels", "description"},
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
