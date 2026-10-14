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

	install := &cobra.Command{Use: "install LIST...", Short: "Add built-in lists to the library", Args: cobra.MinimumNArgs(1),
		Long: "Add built-in lists to the library. Installing a list again skips the\nchannels it already added, so re-running is safe."}
	importFlags(install)
	install.RunE = func(cmd *cobra.Command, args []string) error {
		opts, err := importOptions(cmd)
		if err != nil {
			return err
		}
		lists := make([]seed.List, len(args))
		for i, id := range args {
			if lists[i], err = seed.Get(id); err != nil {
				return err
			}
		}
		return withApp(cmd, func(a *app.Service) error {
			for _, l := range lists {
				b, err := l.Batch()
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "%s: ", l.ID)
				if err := applyImport(cmd, a, b, opts, 0); err != nil {
					return err
				}
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
