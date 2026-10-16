package main

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

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
	root.AddCommand(newInitCommand(), newGroupGoneCommand(), newChannelCommand(), newSharedCommand(), newTagCommand(), newServeCommand(), newImportCommand(), newExportCommand(), newSeedCommand(), newCheckCommand(), newProfileCommand(), newNamesCommand(), newModelCommand(), newRadioCommand(), newSearchCommand(), newOrgCommand(), newSystemCommand(), newPlanCommand())
	return root
}

func newInitCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "init", Short: "Create or upgrade a library database", Args: cobra.NoArgs, RunE: initLibrary,
		Long: `Create a database, or upgrade one to this version of rflib. A new database's
shared list starts with every built-in list (see rflib seed), unless
--no-seed. Upgrading never changes shared channels.`}
	cmd.Flags().Bool("no-seed", false, "Start a new database with an empty shared list")
	return cmd
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
	channel.AddCommand(newChannelAddCommand(), newChannelListCommand(), newChannelShowCommand(), newChannelUpdateCommand(), newChannelStatusCommand(), newChannelRelinkCommand(),
		newChannelCopyCommand())
	return channel
}

func channelFields(cmd *cobra.Command) {
	cmd.Flags().String("name", "", "Channel name")
	cmd.Flags().String("lat", "", "Latitude of where the channel is used, with --lon (empty clears on update)")
	cmd.Flags().String("lon", "", "Longitude, with --lat")
	cmd.Flags().String("radius-km", "", "How far the location reaches, in km: its uncertainty or the coverage (empty clears on update)")
	cmd.Flags().String("grid", "", "A Maidenhead grid square, such as CM94sq, instead of --lat and --lon: its center, reaching its corners unless --radius-km is given")
	cmd.Flags().String("rx", "", "Receive frequency in MHz")
	cmd.Flags().String("tx", "", "Transmit frequency in MHz; omit for receive-only (empty clears on update)")
	cmd.Flags().String("mode", "FM", "FM, NFM, AM, USB, LSB, or CW")
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
	cmd.Flags().String("networks", "", "Comma-separated linking-network nodes, such as allstar:43763,echolink:12345 (networks: allstar, echolink, irlp, wires-x; empty clears on update)")
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
	if set("networks") {
		ns, err := domain.ParseNetworkIDs(splitTags(getString(cmd, "networks")))
		if err != nil {
			return fmt.Errorf("--networks: %w", err)
		}
		c.Networks = ns
	}
	if loc, ok, err := gridLocation(cmd); ok || err != nil {
		if err != nil {
			return err
		}
		c.Location = loc
	} else if set("lat") || set("lon") || set("radius-km") {
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
		Long: "List channels, or the channels matching a query given as arguments or --query.\nRetired channels are left out unless the query says status: (such as\nstatus:retired) or --retired is given.\n\n" + queryHelp + "\n\n" + tableHelp}
	filterFields(cmd)
	cmd.Flags().Bool("retired", false, "Include retired channels")
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
			var cs []domain.Channel
			hidden := 0
			if retired, _ := cmd.Flags().GetBool("retired"); retired {
				cs, err = a.FindChannels(ctx(cmd), n)
			} else {
				cs, hidden, err = a.ListedChannels(ctx(cmd), n)
			}
			if err != nil {
				return err
			}
			if err := channelListing.write(cmd, cs); err != nil {
				return err
			}
			if hidden > 0 {
				fmt.Fprintf(cmd.ErrOrStderr(), "%d retired not shown; add --retired, or search status:retired\n", hidden)
			}
			if more, err := a.MoreInShared(ctx(cmd), n, len(cs)); err == nil && more > 0 {
				fmt.Fprintf(cmd.ErrOrStderr(), "%d more in the shared list; add in:all to include them\n", more)
			}
			return nil
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
var bulkFields = []string{"tags", "skip", "power", "service", "rx-only", "lat", "lon", "radius-km", "grid"}

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
	channelFieldNames := []string{"name", "lat", "lon", "radius-km", "grid", "rx", "tx", "mode", "service", "rx-only", "rx-tone", "tx-tone", "tone-squelch", "power", "bandwidth", "skip", "notes", "tags", "short-names"}
	for _, name := range channelFieldNames {
		if cmd.Flags().Changed(name) && !slices.Contains(bulkFields, name) {
			refused = append(refused, "--"+name)
		}
	}
	if len(refused) > 0 {
		return e, fmt.Errorf("%s can't change many channels at once; update them one at a time with --id, or use --tags, --skip, --power, --service, --rx-only, --lat and --lon, or --grid", strings.Join(refused, ", "))
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
	if loc, ok, err := gridLocation(cmd); ok || err != nil {
		if err != nil {
			return e, err
		}
		e.SetLocation, e.Location = true, loc
	} else if cmd.Flags().Changed("lat") || cmd.Flags().Changed("lon") || cmd.Flags().Changed("radius-km") {
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
		return e, fmt.Errorf("nothing to change; give --tags, --skip, --power, --service, --rx-only, --lat and --lon, or --grid")
	}
	return e, nil
}

// gridLocation reads --grid: the square's center, with --radius-km if given,
// else a radius reaching the square's corners. ok reports whether --grid
// was given.
func gridLocation(cmd *cobra.Command) (loc *domain.Location, ok bool, err error) {
	if !cmd.Flags().Changed("grid") {
		return nil, false, nil
	}
	if cmd.Flags().Changed("lat") || cmd.Flags().Changed("lon") {
		return nil, true, fmt.Errorf("give --grid or --lat and --lon, not both")
	}
	g, err := domain.ParseGrid(getString(cmd, "grid"))
	if err != nil {
		return nil, true, err
	}
	loc = g.Location()
	if cmd.Flags().Changed("radius-km") {
		r, err := parseOptionalFloat("radius-km", getString(cmd, "radius-km"))
		if err != nil {
			return nil, true, err
		}
		loc.RadiusKM = r
	}
	return loc, true, domain.ValidateLocation(*loc)
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

func newChannelStatusCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "status", Short: "Set whether your channels are on the air, or retire them", Args: cobra.NoArgs,
		Long: `Set the status of channels chosen by --id, --query, or both (a channel must
match both): active, off-air (temporarily; --back gives the expected date), or
retired (gone for good; --replaced-by names the channel that took its place,
one of yours or a shared one). Channels are never deleted: a retired one stays
in the library and on radios, crossed out and unprogrammed, and is left out of
channel list unless the search says status:. rflib channel relink then moves
retired channels' places on radios to their replacements.

This is for your own channels; a shared channel's status is the shared list's
(rflib shared status, for its maintainer). --dry-run lists the channels.

` + stdinIDHelp}
	idsFlag(cmd, "Channel IDs")
	cmd.Flags().StringP("query", "q", "", "The channels matching this query (see rflib help search)")
	cmd.Flags().String("set", "", "active, off-air, or retired (required)")
	_ = cmd.MarkFlagRequired("set")
	cmd.Flags().String("note", "", "What's happening, such as \"antenna work\"")
	cmd.Flags().String("back", "", "For off-air: when it's expected back, YYYY-MM-DD")
	cmd.Flags().String("replaced-by", "", "For retired: the channel ID that replaces it")
	cmd.Flags().String("reason", "", "Why, recorded in each channel's history")
	cmd.Flags().Bool("dry-run", false, "List the channels that would change")
	channelListing.flags(cmd)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		sel, err := selection(cmd)
		if err != nil {
			return err
		}
		ch, err := statusChange(cmd)
		if err != nil {
			return err
		}
		return withApp(cmd, func(a *app.Service) error {
			if dryRun, _ := cmd.Flags().GetBool("dry-run"); dryRun {
				cs, err := a.Select(ctx(cmd), sel)
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "Would set %d channels %s\n", len(cs), ch.Status)
				return channelListing.write(cmd, cs)
			}
			cs, err := a.SetStatuses(ctx(cmd), sel, ch, getString(cmd, "reason"))
			if err == nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "Set %d channels %s\n", len(cs), ch.Status)
			}
			return err
		})
	}
	return cmd
}

// statusChange reads --set, --note, --back, and --replaced-by.
func statusChange(cmd *cobra.Command) (app.StatusChange, error) {
	st, err := domain.ParseStatus(getString(cmd, "set"))
	if err != nil {
		return app.StatusChange{}, err
	}
	ch := app.StatusChange{Status: st, Note: getString(cmd, "note"), ReplacedBy: getString(cmd, "replaced-by")}
	if v := getString(cmd, "back"); v != "" {
		t, err := time.Parse(time.DateOnly, v)
		if err != nil {
			return ch, fmt.Errorf("--back must be a date, YYYY-MM-DD")
		}
		ch.BackOn = &t
	}
	return ch, nil
}

func newChannelRelinkCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "relink", Short: "Move retired channels over to their replacements", Args: cobra.NoArgs,
		Long: `Put each retired channel's replacement in its place on your radios, keeping
their settings, for the channels chosen by --id, --query, or both: your own
retired channels, and shared ones (whose notes and tags move over too, as
rflib shared relink does). Channels that aren't retired with a replacement
are left as they are and listed. Program the radios again.

` + stdinIDHelp}
	idsFlag(cmd, "Channel IDs")
	cmd.Flags().StringP("query", "q", "", "The channels matching this query (see rflib help search)")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		sel, err := selection(cmd)
		if err != nil {
			return err
		}
		return withApp(cmd, func(a *app.Service) error {
			moved, left, err := a.RelinkChannels(ctx(cmd), sel)
			if err != nil {
				return err
			}
			for _, m := range moved {
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\n", m.From.Name, m.To.ID, m.To.Name)
			}
			for _, c := range left {
				fmt.Fprintf(cmd.ErrOrStderr(), "left %s: not retired with a replacement\n", c.Name)
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "Relinked %d channels to their replacements\n", len(moved))
			return nil
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
