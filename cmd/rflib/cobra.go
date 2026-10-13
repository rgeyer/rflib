package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ryan/rflib/internal/domain"
	"github.com/ryan/rflib/internal/export"
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
	root.PersistentFlags().String("db", "", "SQLite library path (default $RFLIB_DB, then $XDG_DATA_HOME/rflib/library.db)")
	root.AddCommand(newInitCommand(), newGroupCommand(), newChannelCommand(), newExportCommand())
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

// --- groups ---

func newGroupCommand() *cobra.Command {
	group := &cobra.Command{Use: "group", Short: "Manage geotagged channel groups"}
	group.AddCommand(newGroupAddCommand(), newGroupListCommand(), newGroupShowCommand(), newGroupUpdateCommand(), newGroupDeleteCommand())
	return group
}

func groupFields(cmd *cobra.Command) {
	cmd.Flags().String("name", "", "Group name")
	cmd.Flags().String("description", "", "Description")
	cmd.Flags().String("lat", "", "Latitude (empty clears on update)")
	cmd.Flags().String("lon", "", "Longitude (empty clears on update)")
	cmd.Flags().String("radius-km", "", "Radius in kilometers (empty clears on update)")
	cmd.Flags().String("notes", "", "Notes")
}

// applyGroupFlags copies flags onto g. With all=false only flags the user
// explicitly set are applied, so an update leaves other fields untouched.
func applyGroupFlags(cmd *cobra.Command, g *domain.Group, all bool) error {
	set := func(name string) bool { return all || cmd.Flags().Changed(name) }
	if set("name") {
		g.Name = getString(cmd, "name")
	}
	if set("description") {
		g.Description = getString(cmd, "description")
	}
	if set("notes") {
		g.Notes = getString(cmd, "notes")
	}
	for _, f := range []struct {
		name string
		dst  **float64
	}{{"lat", &g.Latitude}, {"lon", &g.Longitude}, {"radius-km", &g.RadiusKM}} {
		if set(f.name) {
			v, err := parseOptionalFloat(f.name, getString(cmd, f.name))
			if err != nil {
				return err
			}
			*f.dst = v
		}
	}
	return nil
}

func newGroupAddCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "add", Short: "Create a group", Args: cobra.NoArgs}
	groupFields(cmd)
	_ = cmd.MarkFlagRequired("name")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		var g domain.Group
		if err := applyGroupFlags(cmd, &g, true); err != nil {
			return err
		}
		return withStore(cmd, func(s *store.Store) error {
			g, err := s.CreateGroup(ctx(cmd), g)
			if err == nil {
				fmt.Fprintln(cmd.OutOrStdout(), g.ID)
			}
			return err
		})
	}
	return cmd
}

func newGroupListCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "list", Short: "List groups", Args: cobra.NoArgs}
	formatFlag(cmd)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		format, err := getFormat(cmd)
		if err != nil {
			return err
		}
		return withStore(cmd, func(s *store.Store) error {
			gs, err := s.ListGroups(ctx(cmd))
			if err != nil {
				return err
			}
			return writeGroupList(cmd.OutOrStdout(), gs, format)
		})
	}
	return cmd
}

func newGroupShowCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "show", Short: "Show a group", Args: cobra.NoArgs}
	idFlag(cmd, "Group ID or name (required)")
	formatFlag(cmd)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		format, err := getFormat(cmd)
		if err != nil {
			return err
		}
		return withStore(cmd, func(s *store.Store) error {
			g, err := s.GetGroup(ctx(cmd), getString(cmd, "id"))
			if err != nil {
				return err
			}
			return writeGroup(cmd.OutOrStdout(), g, format)
		})
	}
	return cmd
}

func newGroupUpdateCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "update", Short: "Update a group; only supplied flags change", Args: cobra.NoArgs}
	idFlag(cmd, "Group ID or name (required)")
	groupFields(cmd)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		return withStore(cmd, func(s *store.Store) error {
			g, err := s.GetGroup(ctx(cmd), getString(cmd, "id"))
			if err != nil {
				return err
			}
			if err := applyGroupFlags(cmd, &g, false); err != nil {
				return err
			}
			_, err = s.UpdateGroup(ctx(cmd), g)
			return err
		})
	}
	return cmd
}

func newGroupDeleteCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "delete", Short: "Delete a group; its channels become ungrouped", Args: cobra.NoArgs}
	idFlag(cmd, "Group ID or name (required)")
	cmd.Flags().Bool("yes", false, "Confirm deletion")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if err := requireYes(cmd, "group"); err != nil {
			return err
		}
		return withStore(cmd, func(s *store.Store) error { return s.DeleteGroup(ctx(cmd), getString(cmd, "id")) })
	}
	return cmd
}

// --- channels ---

func newChannelCommand() *cobra.Command {
	channel := &cobra.Command{Use: "channel", Short: "Manage analog radio channels"}
	channel.AddCommand(newChannelAddCommand(), newChannelListCommand(), newChannelShowCommand(), newChannelUpdateCommand(), newChannelDeleteCommand())
	return channel
}

func channelFields(cmd *cobra.Command) {
	cmd.Flags().String("name", "", "Channel name")
	cmd.Flags().String("group", "", "Group ID or name (empty clears on update)")
	cmd.Flags().String("rx", "", "Receive frequency in MHz")
	cmd.Flags().String("tx", "", "Transmit frequency in MHz; omit for receive-only (empty clears on update)")
	cmd.Flags().String("mode", "FM", "FM, NFM, or AM")
	cmd.Flags().String("rx-tone", "none", "none, ctcss:<Hz>, or dcs:<code>[:inverted]")
	cmd.Flags().String("tx-tone", "none", "none, ctcss:<Hz>, or dcs:<code>[:inverted]")
	cmd.Flags().Bool("tone-squelch", false, "Enable receive tone squelch")
	cmd.Flags().String("power", "", "Power label")
	cmd.Flags().String("bandwidth", "", "Bandwidth label")
	cmd.Flags().Bool("skip", false, "Skip when scanning")
	cmd.Flags().String("notes", "", "Notes")
	cmd.Flags().String("tags", "", "Comma-separated tags (empty clears on update)")
}

// applyChannelFlags copies flags onto c. With all=false only flags the user
// explicitly set are applied; an explicitly empty --tx, --group, or --tags
// clears that field.
func applyChannelFlags(cmd *cobra.Command, s *store.Store, c *domain.Channel, all bool) error {
	set := func(name string) bool { return all || cmd.Flags().Changed(name) }
	for _, f := range []struct {
		name string
		dst  *string
	}{{"name", &c.Name}, {"mode", &c.Mode}, {"power", &c.Power}, {"bandwidth", &c.Bandwidth}, {"notes", &c.Notes}} {
		if set(f.name) {
			*f.dst = getString(cmd, f.name)
		}
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
	if set("tags") {
		c.Tags = splitTags(getString(cmd, "tags"))
	}
	if set("group") {
		c.GroupID, c.Group = nil, nil
		if ref := getString(cmd, "group"); strings.TrimSpace(ref) != "" {
			g, err := s.GetGroup(ctx(cmd), ref)
			if err != nil {
				return err
			}
			c.GroupID, c.Group = &g.ID, &g
		}
	}
	return nil
}

func newChannelAddCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "add", Short: "Create an analog channel", Args: cobra.NoArgs}
	channelFields(cmd)
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("rx")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		return withStore(cmd, func(s *store.Store) error {
			var c domain.Channel
			if err := applyChannelFlags(cmd, s, &c, true); err != nil {
				return err
			}
			c, err := s.CreateChannel(ctx(cmd), c)
			if err == nil {
				fmt.Fprintln(cmd.OutOrStdout(), c.ID)
			}
			return err
		})
	}
	return cmd
}

func filterFields(cmd *cobra.Command) {
	cmd.Flags().String("group", "", "Group ID or name")
	cmd.Flags().String("tag", "", "Tag")
	cmd.Flags().String("lat", "", "Latitude")
	cmd.Flags().String("lon", "", "Longitude")
	cmd.Flags().String("radius-km", "", "Radius in kilometers")
}

func channelFilter(cmd *cobra.Command, s *store.Store) (store.ChannelFilter, error) {
	f := store.ChannelFilter{Tag: getString(cmd, "tag")}
	if ref := getString(cmd, "group"); ref != "" {
		g, err := s.GetGroup(ctx(cmd), ref)
		if err != nil {
			return f, err
		}
		f.GroupID = g.ID
	}
	var err error
	for _, x := range []struct {
		name string
		dst  **float64
	}{{"lat", &f.Latitude}, {"lon", &f.Longitude}, {"radius-km", &f.RadiusKM}} {
		if *x.dst, err = parseOptionalFloat(x.name, getString(cmd, x.name)); err != nil {
			return f, err
		}
	}
	return f, nil
}

func newChannelListCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "list", Short: "List channels", Args: cobra.NoArgs}
	filterFields(cmd)
	formatFlag(cmd)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		format, err := getFormat(cmd)
		if err != nil {
			return err
		}
		return withStore(cmd, func(s *store.Store) error {
			filter, err := channelFilter(cmd, s)
			if err != nil {
				return err
			}
			cs, err := s.ListChannels(ctx(cmd), filter)
			if err != nil {
				return err
			}
			return writeChannelList(cmd.OutOrStdout(), cs, format)
		})
	}
	return cmd
}

func newChannelShowCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "show", Short: "Show a channel", Args: cobra.NoArgs}
	idFlag(cmd, "Channel ID (required)")
	formatFlag(cmd)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		format, err := getFormat(cmd)
		if err != nil {
			return err
		}
		return withStore(cmd, func(s *store.Store) error {
			c, err := s.GetChannel(ctx(cmd), getString(cmd, "id"))
			if err != nil {
				return err
			}
			return writeChannel(cmd.OutOrStdout(), c, format)
		})
	}
	return cmd
}

func newChannelUpdateCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "update", Short: "Update a channel; only supplied flags change", Args: cobra.NoArgs}
	idFlag(cmd, "Channel ID (required)")
	channelFields(cmd)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		return withStore(cmd, func(s *store.Store) error {
			c, err := s.GetChannel(ctx(cmd), getString(cmd, "id"))
			if err != nil {
				return err
			}
			if err := applyChannelFlags(cmd, s, &c, false); err != nil {
				return err
			}
			_, err = s.UpdateChannel(ctx(cmd), c)
			return err
		})
	}
	return cmd
}

func newChannelDeleteCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "delete", Short: "Delete a channel", Args: cobra.NoArgs}
	idFlag(cmd, "Channel ID (required)")
	cmd.Flags().Bool("yes", false, "Confirm deletion")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if err := requireYes(cmd, "channel"); err != nil {
			return err
		}
		return withStore(cmd, func(s *store.Store) error { return s.DeleteChannel(ctx(cmd), getString(cmd, "id")) })
	}
	return cmd
}

// --- export ---

func newExportCommand() *cobra.Command {
	exp := &cobra.Command{Use: "export", Short: "Export library records"}
	csv := &cobra.Command{Use: "csv", Short: "Export normalized generic CSV", Args: cobra.NoArgs}
	csv.Flags().String("output", "-", "Output CSV path, or - for stdout")
	filterFields(csv)
	csv.RunE = func(cmd *cobra.Command, _ []string) error {
		return withStore(cmd, func(s *store.Store) error {
			filter, err := channelFilter(cmd, s)
			if err != nil {
				return err
			}
			cs, err := s.ListChannels(ctx(cmd), filter)
			if err != nil {
				return err
			}
			return writeExport(cmd, getString(cmd, "output"), export.GenericCSV{}, cs)
		})
	}
	exp.AddCommand(csv)
	return exp
}

// writeExport writes to stdout or a file. A failed file export is removed so
// it can't be mistaken for a complete one. The summary goes to stderr so it
// never mixes with exported data on stdout.
func writeExport(cmd *cobra.Command, path string, ex export.Exporter, cs []domain.Channel) error {
	if path == "" || path == "-" {
		return ex.Export(cmd.OutOrStdout(), cs)
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	err = ex.Export(file, cs)
	if cerr := file.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(path)
		return err
	}
	fmt.Fprintln(cmd.ErrOrStderr(), "Exported", len(cs), "channels to", path)
	return nil
}
