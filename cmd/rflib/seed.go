package main

import (
	"fmt"

	"github.com/ryan/rflib/internal/app"
	"github.com/ryan/rflib/internal/domain"
	"github.com/ryan/rflib/internal/seed"
	"github.com/spf13/cobra"
)

func newSeedCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "seed", Short: "Install built-in reference channel lists"}

	list := &cobra.Command{Use: "list", Short: "List built-in channel lists", Args: cobra.NoArgs}
	list.RunE = func(cmd *cobra.Command, _ []string) error {
		for _, l := range seed.Lists() {
			b, err := l.Batch()
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s\t%d channels\t%s\n", l.ID, len(b.Records), l.Description)
		}
		return nil
	}

	show := &cobra.Command{Use: "show LIST", Short: "Show the channels in a built-in list", Args: cobra.ExactArgs(1)}
	formatFlag(show)
	show.RunE = func(cmd *cobra.Command, args []string) error {
		format, err := getFormat(cmd)
		if err != nil {
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
		if format == "json" {
			return writeChannelList(cmd.OutOrStdout(), cs, format)
		}
		for _, c := range cs {
			fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\t%s\n", c.Name, domain.FormatFrequencyMHz(c.ReceiveHz), txText(c), c.Mode, c.Notes)
		}
		return nil
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
				fmt.Fprintf(cmd.OutOrStdout(), "%s: ", l.ID)
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

func newCheckCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "check", Short: "Report channels that don't fit their service's US band plan", Args: cobra.NoArgs,
		Long: `Report channels that don't fit their service's US band plan: amateur
transmit frequencies outside the amateur bands, off-plan FRS, GMRS, MURS, CB,
and NOAA channels, transmitting NOAA or public safety channels, and channels
without a service that transmit outside the amateur bands and FRS, GMRS, MURS,
and CB channels. Exits non-zero when anything is reported.`}
	filterFields(cmd)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		return withApp(cmd, func(a *app.Service) error {
			cs, err := filteredChannels(cmd, a)
			if err != nil {
				return err
			}
			n := 0
			for _, c := range cs {
				for _, w := range domain.CheckBandPlan(c) {
					n++
					fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\n", c.ID, c.Name, w)
				}
			}
			if n > 0 {
				return fmt.Errorf("%d band plan warnings", n)
			}
			return nil
		})
	}
	return cmd
}
