package main

import (
	"encoding/json"
	"fmt"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/ryan/rflib/internal/buildinfo"
)

func newVersionCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "version", Short: "Say which rflib this is", Args: cobra.NoArgs,
		Long: `Prints rflib's version, the commit it was built from, the Go it was built
with, and the system it's for. Include it in bug reports.`}
	cmd.Flags().String("format", "text", "Output format: text or json")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		info := buildinfo.Get()
		platform := runtime.GOOS + "/" + runtime.GOARCH
		switch getString(cmd, "format") {
		case "json":
			out := struct {
				buildinfo.Info
				Platform string `json:"platform"`
			}{info, platform}
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(out)
		case "text":
			fmt.Fprintf(cmd.OutOrStdout(), "rflib %s\n", info.Version)
			if info.Commit != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "commit %s", info.Commit)
				if !info.Time.IsZero() {
					fmt.Fprintf(cmd.OutOrStdout(), " (%s)", info.Time.UTC().Format("2006-01-02 15:04 UTC"))
				}
				fmt.Fprintln(cmd.OutOrStdout())
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s, %s\n", info.Go, platform)
			return nil
		}
		return fmt.Errorf("--format is text or json")
	}
	return cmd
}
