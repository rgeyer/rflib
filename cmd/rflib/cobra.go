package main

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
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
	root.AddCommand(newInitCommand(), newGroupCommand(), newChannelCommand(), newExportCommand())
	return root
}

func newInitCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "init", Short: "Create or upgrade a library database"}
	cmd.Flags().String("db", "", "SQLite library path (required)")
	cmd.RunE = legacyRun(initCmd)
	return cmd
}

func newGroupCommand() *cobra.Command {
	group := &cobra.Command{Use: "group", Short: "Manage geotagged channel groups"}
	group.AddCommand(newGroupAddCommand(), newGroupListCommand(), newGroupShowCommand(), newGroupUpdateCommand(), newGroupDeleteCommand())
	return group
}

func newGroupAddCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "add", Short: "Create a group"}
	groupFields(cmd, true)
	cmd.RunE = legacyRun(groupAdd)
	return cmd
}
func newGroupListCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "list", Short: "List groups"}
	cmd.Flags().String("db", "", "SQLite library path (required)")
	cmd.RunE = legacyRun(groupList)
	return cmd
}
func newGroupShowCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "show", Short: "Show a group"}
	cmd.Flags().String("db", "", "SQLite library path (required)")
	cmd.Flags().String("id", "", "Group ID or name (required)")
	cmd.RunE = legacyRun(groupShow)
	return cmd
}
func newGroupUpdateCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "update", Short: "Update a group"}
	groupFields(cmd, true)
	cmd.Flags().String("id", "", "Group ID or name (required)")
	cmd.RunE = legacyRun(groupUpdate)
	return cmd
}
func newGroupDeleteCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "delete", Short: "Delete a group"}
	cmd.Flags().String("db", "", "SQLite library path (required)")
	cmd.Flags().String("id", "", "Group ID (required)")
	cmd.Flags().Bool("yes", false, "Confirm deletion")
	cmd.RunE = legacyRun(groupDelete)
	return cmd
}
func groupFields(cmd *cobra.Command, includeDB bool) {
	if includeDB {
		cmd.Flags().String("db", "", "SQLite library path (required)")
	}
	cmd.Flags().String("name", "", "Group name")
	cmd.Flags().String("description", "", "Description")
	cmd.Flags().String("lat", "", "Latitude")
	cmd.Flags().String("lon", "", "Longitude")
	cmd.Flags().String("radius-km", "", "Radius in kilometers")
	cmd.Flags().String("notes", "", "Notes")
}

func newChannelCommand() *cobra.Command {
	channel := &cobra.Command{Use: "channel", Short: "Manage analog radio channels"}
	channel.AddCommand(newChannelAddCommand(), newChannelListCommand(), newChannelShowCommand(), newChannelUpdateCommand(), newChannelDeleteCommand())
	return channel
}
func newChannelAddCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "add", Short: "Create an analog channel"}
	channelFields(cmd)
	cmd.RunE = legacyRun(channelAdd)
	return cmd
}
func newChannelListCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "list", Short: "List channels"}
	cmd.Flags().String("db", "", "SQLite library path (required)")
	filterFields(cmd)
	cmd.RunE = legacyRun(channelList)
	return cmd
}
func newChannelShowCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "show", Short: "Show a channel"}
	cmd.Flags().String("db", "", "SQLite library path (required)")
	cmd.Flags().String("id", "", "Channel ID (required)")
	cmd.RunE = legacyRun(channelShow)
	return cmd
}
func newChannelUpdateCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "update", Short: "Update an analog channel"}
	channelFields(cmd)
	cmd.Flags().String("id", "", "Channel ID (required)")
	cmd.RunE = legacyRun(channelUpdate)
	return cmd
}
func newChannelDeleteCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "delete", Short: "Delete a channel"}
	cmd.Flags().String("db", "", "SQLite library path (required)")
	cmd.Flags().String("id", "", "Channel ID (required)")
	cmd.Flags().Bool("yes", false, "Confirm deletion")
	cmd.RunE = legacyRun(channelDelete)
	return cmd
}
func channelFields(cmd *cobra.Command) {
	cmd.Flags().String("db", "", "SQLite library path (required)")
	cmd.Flags().String("name", "", "Channel name")
	cmd.Flags().String("group", "", "Group ID or name")
	cmd.Flags().String("rx", "", "Receive frequency in MHz")
	cmd.Flags().String("tx", "", "Transmit frequency in MHz; omit for receive-only")
	cmd.Flags().String("mode", "FM", "FM, NFM, or AM")
	cmd.Flags().String("rx-tone", "none", "none, ctcss:<Hz>, or dcs:<code>[:inverted]")
	cmd.Flags().String("tx-tone", "none", "none, ctcss:<Hz>, or dcs:<code>[:inverted]")
	cmd.Flags().Bool("tone-squelch", false, "Enable receive tone squelch")
	cmd.Flags().String("power", "", "Power label")
	cmd.Flags().String("bandwidth", "", "Bandwidth label")
	cmd.Flags().Bool("skip", false, "Skip when scanning")
	cmd.Flags().String("notes", "", "Notes")
	cmd.Flags().String("tags", "", "Comma-separated tags")
}

func newExportCommand() *cobra.Command {
	export := &cobra.Command{Use: "export", Short: "Export library records"}
	csv := &cobra.Command{Use: "csv", Short: "Export normalized generic CSV"}
	csv.Flags().String("db", "", "SQLite library path (required)")
	csv.Flags().String("output", "", "Output CSV path (required)")
	filterFields(csv)
	csv.RunE = legacyRun(func(args []string, out io.Writer) error { return exportCmd(append([]string{"csv"}, args...), out) })
	export.AddCommand(csv)
	return export
}
func filterFields(cmd *cobra.Command) {
	cmd.Flags().String("group", "", "Group ID")
	cmd.Flags().String("tag", "", "Tag")
	cmd.Flags().String("lat", "", "Latitude")
	cmd.Flags().String("lon", "", "Longitude")
	cmd.Flags().String("radius-km", "", "Radius in kilometers")
}

func legacyRun(handler func([]string, io.Writer) error) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) > 0 {
			return fmt.Errorf("unexpected arguments: %v", args)
		}
		if help := cmd.Flags().Lookup("help"); help != nil && help.Value.String() == "true" {
			return cmd.Help()
		}
		return handler(cobraFlagArgs(cmd), cmd.OutOrStdout())
	}
}
func cobraFlagArgs(cmd *cobra.Command) []string {
	args := make([]string, 0)
	cmd.Flags().VisitAll(func(flag *pflag.Flag) {
		if flag.Name != "help" {
			args = append(args, "--"+flag.Name+"="+flag.Value.String())
		}
	})
	return args
}
