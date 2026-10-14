package main

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/ryan/rflib/internal/app"
	"github.com/ryan/rflib/internal/domain"
	"github.com/ryan/rflib/internal/store"
	"github.com/spf13/cobra"
)

func newRootCmd(out, errOut io.Writer) *cobra.Command {
	root := &cobra.Command{
		Use:           "rflib",
		Short:         "Manage a local SQLite library of analog radio channels",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetOut(out)
	root.SetErr(errOut)
	root.PersistentFlags().String("db", "", "SQLite database path (default $RFLIB_DB, then $XDG_DATA_HOME/rflib/library.db)")
	root.PersistentFlags().String("library", "", "Library to use, by name or ID (default $RFLIB_LIBRARY, then the default library; see rflib library)")
	root.AddCommand(newInitCommand(), newGroupGoneCommand(), newChannelCommand(), newSharedCommand(), newTagCommand(), newServeCommand(), newImportCommand(), newExportCommand(), newSeedCommand(), newCheckCommand(), newProfileCommand(), newNamesCommand(), newModelCommand(), newRadioCommand(), newSearchCommand(), newLibraryCommand())
	return root
}

func newInitCommand() *cobra.Command {
	return &cobra.Command{Use: "init", Short: "Create or upgrade a library database", Args: cobra.NoArgs, RunE: initLibrary}
}

// formatFlag adds --format to a read command.
func formatFlag(cmd *cobra.Command) {
	cmd.Flags().String("format", "text", "Output format: text or json")
}

func getFormat(cmd *cobra.Command) (string, error) {
	f, _ := cmd.Flags().GetString("format")
	return f, checkFormat(f)
}

func idFlag(cmd *cobra.Command, usage string) {
	cmd.Flags().String("id", "", usage)
	_ = cmd.MarkFlagRequired("id")
}

func getString(cmd *cobra.Command, name string) string {
	v, _ := cmd.Flags().GetString(name)
	return v
}

func requireYes(cmd *cobra.Command, what string) error {
	if yes, _ := cmd.Flags().GetBool("yes"); !yes {
		return fmt.Errorf("%s deletion requires --yes", what)
	}
	return nil
}

// newGroupGoneCommand explains where groups went to anyone who still
// types rflib group.
func newGroupGoneCommand() *cobra.Command {
	return &cobra.Command{Use: "group", Hidden: true, DisableFlagParsing: true, RunE: func(*cobra.Command, []string) error {
		return fmt.Errorf("groups are gone: each became a tag named after it, and channels now have their own location (channel update --lat --lon --radius-km)")
	}}
}

// --- channels ---

func newChannelCommand() *cobra.Command {
	channel := &cobra.Command{Use: "channel", Short: "Manage analog radio channels"}
	channel.AddCommand(newChannelAddCommand(), newChannelListCommand(), newChannelShowCommand(), newChannelUpdateCommand(), newChannelDeleteCommand(),
		newChannelLinkCommand(), newChannelCopyCommand())
	return channel
}

func channelFields(cmd *cobra.Command) {
	cmd.Flags().String("name", "", "Channel name")
	cmd.Flags().String("lat", "", "Latitude of where the channel is used, with --lon (empty clears on update)")
	cmd.Flags().String("lon", "", "Longitude, with --lat")
	cmd.Flags().String("radius-km", "", "How far the location reaches, in km: its uncertainty or the coverage (empty clears on update)")
	cmd.Flags().String("rx", "", "Receive frequency in MHz")
	cmd.Flags().String("tx", "", "Transmit frequency in MHz; omit for receive-only (empty clears on update)")
	cmd.Flags().String("mode", "FM", "FM, NFM, AM, USB, or LSB")
	cmd.Flags().String("service", "", "Radio service: amateur, gmrs, frs, cb, murs, marine, noaa, public_safety, other (empty clears on update)")
	cmd.Flags().Bool("rx-only", false, "Never transmit; exports program the channel receive only (default on for noaa and public_safety)")
	cmd.Flags().String("rx-tone", "none", "none, ctcss:<Hz>, or dcs:<code>[:inverted]")
	cmd.Flags().String("tx-tone", "none", "none, ctcss:<Hz>, or dcs:<code>[:inverted]")
	cmd.Flags().Bool("tone-squelch", false, "Enable receive tone squelch")
	cmd.Flags().String("power", "", "Power intent: low, medium, high, or max; each radio maps it to its own levels (empty clears on update)")
	cmd.Flags().String("bandwidth", "", "Bandwidth label")
	cmd.Flags().Bool("skip", false, "Skip when scanning")
	cmd.Flags().String("notes", "", "Notes")
	cmd.Flags().String("tags", "", "Comma-separated tags; on update, +x,-y adds x and removes y (empty clears on update)")
	cmd.Flags().String("short-names", "", "Comma-separated abbreviations of the name for narrow radio displays, such as \"Metro Rpt,MtRpt\" (empty clears on update)")
}

// applyChannelFlags copies flags onto c. With all=false only flags the user
// explicitly set are applied; an explicitly empty --tx, --lat/--lon, or --tags
// clears that field.
func applyChannelFlags(cmd *cobra.Command, a *app.Service, c *domain.Channel, all bool) error {
	set := func(name string) bool { return all || cmd.Flags().Changed(name) }
	for _, f := range []struct {
		name string
		dst  *string
	}{{"name", &c.Name}, {"mode", &c.Mode}, {"bandwidth", &c.Bandwidth}, {"notes", &c.Notes}} {
		if set(f.name) {
			*f.dst = getString(cmd, f.name)
		}
	}
	if set("power") {
		p, err := domain.ParsePowerIntent(getString(cmd, "power"))
		if err != nil {
			return err
		}
		c.Power = p
	}
	if set("rx") {
		hz, err := domain.ParseFrequencyMHz(getString(cmd, "rx"))
		if err != nil {
			return fmt.Errorf("--rx: %w", err)
		}
		c.ReceiveHz = hz
	}
	if set("tx") {
		c.TransmitHz = nil
		if v := getString(cmd, "tx"); strings.TrimSpace(v) != "" {
			hz, err := domain.ParseFrequencyMHz(v)
			if err != nil {
				return fmt.Errorf("--tx: %w", err)
			}
			c.TransmitHz = &hz
		}
	}
	for _, f := range []struct {
		name string
		dst  *domain.Tone
	}{{"rx-tone", &c.ReceiveTone}, {"tx-tone", &c.TransmitTone}} {
		if set(f.name) {
			t, err := domain.ParseTone(getString(cmd, f.name))
			if err != nil {
				return fmt.Errorf("--%s: %w", f.name, err)
			}
			*f.dst = t
		}
	}
	if set("tone-squelch") {
		c.ToneSquelch, _ = cmd.Flags().GetBool("tone-squelch")
	}
	if set("skip") {
		c.Skip, _ = cmd.Flags().GetBool("skip")
	}
	if set("service") {
		svc, err := domain.ParseService(getString(cmd, "service"))
		if err != nil {
			return err
		}
		c.Service = svc
		// A listen-only service turns rx-only on unless --rx-only says
		// otherwise; it never turns it off.
		c.RXOnly = c.RXOnly || domain.DefaultRXOnly(svc)
	}
	if cmd.Flags().Changed("rx-only") {
		c.RXOnly, _ = cmd.Flags().GetBool("rx-only")
	}
	if set("tags") {
		e, err := parseTagEdit(getString(cmd, "tags"))
		if err != nil {
			return err
		}
		c.Tags = e.Apply(c.Tags)
	}
	if set("short-names") {
		c.ShortNames = splitTags(getString(cmd, "short-names"))
	}
	if set("lat") || set("lon") || set("radius-km") {
		var lat, lon, radius *float64
		if l := c.Location; l != nil {
			lat, lon, radius = &l.Latitude, &l.Longitude, l.RadiusKM
		}
		for _, f := range []struct {
			name string
			dst  **float64
		}{{"lat", &lat}, {"lon", &lon}, {"radius-km", &radius}} {
			if set(f.name) {
				v, err := parseOptionalFloat(f.name, getString(cmd, f.name))
				if err != nil {
					return err
				}
				*f.dst = v
			}
		}
		if lat == nil && lon == nil {
			radius = nil
		}
		loc, err := domain.NewLocation(lat, lon, radius)
		if err != nil {
			return err
		}
		c.Location = loc
	}
	return nil
}

func newChannelAddCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "add", Short: "Create an analog channel", Args: cobra.NoArgs}
	channelFields(cmd)
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("rx")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		return withApp(cmd, func(a *app.Service) error {
			var c domain.Channel
			if err := applyChannelFlags(cmd, a, &c, true); err != nil {
				return err
			}
			c, warnings, err := a.CreateChannel(ctx(cmd), c)
			if err == nil {
				fmt.Fprintln(cmd.OutOrStdout(), c.ID)
				warnBandPlan(cmd, c, warnings)
			}
			return err
		})
	}
	return cmd
}

func newChannelListCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "list [QUERY...]", Short: "List or search channels",
		Long: "List channels, or the channels matching a query given as arguments or --query.\n\n" + queryHelp + "\n\n" + tableHelp}
	filterFields(cmd)
	channelListing.flags(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := channelListing.check(cmd); err != nil {
			return err
		}
		n, err := channelQuery(cmd, args)
		if err != nil {
			return err
		}
		return withApp(cmd, func(a *app.Service) error {
			cs, err := a.FindChannels(ctx(cmd), n)
			if err != nil {
				return err
			}
			return channelListing.write(cmd, cs)
		})
	}
	return cmd
}

func newChannelShowCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "show", Short: "Show a channel", Args: cobra.NoArgs}
	idFlag(cmd, "Channel ID, a unique prefix of one, or - to read it from stdin (required)")
	formatFlag(cmd)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		format, err := getFormat(cmd)
		if err != nil {
			return err
		}
		id, err := getID(cmd)
		if err != nil {
			return err
		}
		show := func(a *app.Service) error {
			c, err := a.GetChannel(ctx(cmd), id)
			if err != nil {
				return err
			}
			return writeChannel(cmd.OutOrStdout(), c, format)
		}
		err = withApp(cmd, show)
		// A shared channel's ID, as channel list in:shared prints it.
		if errors.Is(err, store.ErrNotFound) {
			if shErr := withShared(cmd, show); shErr == nil {
				return nil
			}
		}
		return err
	}
	return cmd
}

// bulkFields are the channel flags that can change many channels at once.
var bulkFields = []string{"tags", "skip", "power", "service", "rx-only", "lat", "lon", "radius-km"}

func newChannelUpdateCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "update", Short: "Update channels; only supplied flags change", Args: cobra.NoArgs,
		Long: `Update channels; only supplied flags change, and an empty value clears a
field. Choose channels with --id, --query, or both (a channel must match
both).

One channel can change anything. Many at once (--query, or several IDs) can
change only --tags, --skip, --power, --service, --rx-only, and the location
(--lat and --lon together, with --radius-km if wanted): frequencies, tones,
mode, and names are changed one channel at a time. --tags +x,-y adds
x and removes y, leaving other tags alone; --tags x,y replaces them.

` + stdinIDHelp}
	idsFlag(cmd, "Channel IDs")
	cmd.Flags().StringP("query", "q", "", "Update the channels matching this query (see rflib help search)")
	cmd.Flags().Bool("dry-run", false, "List the channels that would change, without changing them")
	channelFields(cmd)
	channelListing.flags(cmd)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		sel, err := selection(cmd)
		if err != nil {
			return err
		}
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		if !sel.Bulk() {
			return withApp(cmd, func(a *app.Service) error {
				c, err := a.GetChannel(ctx(cmd), sel.IDs[0])
				if err != nil {
					return err
				}
				if err := applyChannelFlags(cmd, a, &c, false); err != nil {
					return err
				}
				if dryRun {
					return channelListing.write(cmd, []domain.Channel{c})
				}
				c, warnings, err := a.UpdateChannel(ctx(cmd), c)
				if err == nil {
					warnBandPlan(cmd, c, warnings)
				}
				return err
			})
		}
		edit, err := channelEdit(cmd)
		if err != nil {
			return err
		}
		return withApp(cmd, func(a *app.Service) error {
			if dryRun {
				cs, err := a.Select(ctx(cmd), sel)
				if err != nil {
					return err
				}
				for i := range cs {
					edit.Apply(&cs[i])
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "Would update %d channels\n", len(cs))
				return channelListing.write(cmd, cs)
			}
			cs, warnings, err := a.UpdateChannels(ctx(cmd), sel, edit)
			if err != nil {
				return err
			}
			for _, w := range warnings {
				fmt.Fprintln(cmd.ErrOrStderr(), "warning:", w)
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "Updated %d channels\n", len(cs))
			return nil
		})
	}
	return cmd
}

// channelEdit reads a bulk update's flags, refusing any outside bulkFields.
func channelEdit(cmd *cobra.Command) (app.ChannelEdit, error) {
	var e app.ChannelEdit
	var refused []string
	channelFieldNames := []string{"name", "lat", "lon", "radius-km", "rx", "tx", "mode", "service", "rx-only", "rx-tone", "tx-tone", "tone-squelch", "power", "bandwidth", "skip", "notes", "tags", "short-names"}
	for _, name := range channelFieldNames {
		if cmd.Flags().Changed(name) && !slices.Contains(bulkFields, name) {
			refused = append(refused, "--"+name)
		}
	}
	if len(refused) > 0 {
		return e, fmt.Errorf("%s can't change many channels at once; update them one at a time with --id, or use --tags, --skip, --power, --service, --rx-only, or --lat and --lon", strings.Join(refused, ", "))
	}
	if cmd.Flags().Changed("tags") {
		t, err := parseTagEdit(getString(cmd, "tags"))
		if err != nil {
			return e, err
		}
		e.Tags = &t
	}
	if cmd.Flags().Changed("skip") {
		v, _ := cmd.Flags().GetBool("skip")
		e.Skip = &v
	}
	if cmd.Flags().Changed("power") {
		p, err := domain.ParsePowerIntent(getString(cmd, "power"))
		if err != nil {
			return e, err
		}
		e.Power = &p
	}
	if cmd.Flags().Changed("service") {
		svc, err := domain.ParseService(getString(cmd, "service"))
		if err != nil {
			return e, err
		}
		e.Service = &svc
	}
	if cmd.Flags().Changed("rx-only") {
		v, _ := cmd.Flags().GetBool("rx-only")
		e.RXOnly = &v
	}
	if cmd.Flags().Changed("lat") || cmd.Flags().Changed("lon") || cmd.Flags().Changed("radius-km") {
		if !cmd.Flags().Changed("lat") || !cmd.Flags().Changed("lon") {
			return e, fmt.Errorf("a location for many channels needs --lat and --lon (both empty to clear it)")
		}
		var parts [3]*float64
		for i, name := range []string{"lat", "lon", "radius-km"} {
			v, err := parseOptionalFloat(name, getString(cmd, name))
			if err != nil {
				return e, err
			}
			parts[i] = v
		}
		loc, err := domain.NewLocation(parts[0], parts[1], parts[2])
		if err != nil {
			return e, err
		}
		e.SetLocation, e.Location = true, loc
	}
	if e == (app.ChannelEdit{}) {
		return e, fmt.Errorf("nothing to change; give --tags, --skip, --power, --service, --rx-only, or --lat and --lon")
	}
	return e, nil
}

// parseTagEdit reads --tags: "+x,-y" adds and removes tags; anything else
// replaces them, and an empty value clears them.
func parseTagEdit(v string) (app.TagEdit, error) {
	items := splitTags(v)
	var e app.TagEdit
	edits := 0
	for _, t := range items {
		t = strings.TrimSpace(t)
		switch {
		case strings.HasPrefix(t, "+"):
			e.Add = append(e.Add, t[1:])
			edits++
		case strings.HasPrefix(t, "-"):
			e.Remove = append(e.Remove, t[1:])
			edits++
		}
	}
	switch {
	case edits == 0:
		e.Set = append([]string{}, items...)
	case edits < len(items):
		return e, fmt.Errorf("--tags %q mixes +/- edits with plain tags; use +tag and -tag for every tag, or none", v)
	}
	return e, nil
}

func newChannelDeleteCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "delete", Short: "Delete channels, taking them off every radio", Args: cobra.NoArgs,
		Long: `Delete channels by --id, --query, or both (a channel must match both). They
come off every radio too. Needs --yes; --dry-run lists what would go.

` + stdinIDHelp}
	idsFlag(cmd, "Channel IDs")
	cmd.Flags().StringP("query", "q", "", "Delete the channels matching this query (see rflib help search)")
	cmd.Flags().Bool("dry-run", false, "List the channels that would be deleted")
	cmd.Flags().Bool("yes", false, "Confirm deletion")
	channelListing.flags(cmd)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		sel, err := selection(cmd)
		if err != nil {
			return err
		}
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		if !dryRun {
			if err := requireYes(cmd, "channel"); err != nil {
				return err
			}
		}
		return withApp(cmd, func(a *app.Service) error {
			if dryRun {
				cs, err := a.Select(ctx(cmd), sel)
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "Would delete %d channels\n", len(cs))
				return channelListing.write(cmd, cs)
			}
			cs, err := a.DeleteChannels(ctx(cmd), sel)
			if err == nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "Deleted %d channels\n", len(cs))
			}
			return err
		})
	}
	return cmd
}

// warnBandPlan prints a saved channel's band-plan warnings to stderr.
func warnBandPlan(cmd *cobra.Command, c domain.Channel, warnings []string) {
	for _, w := range warnings {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s: %s\n", c.Name, w)
	}
}
