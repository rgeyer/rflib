package main

import (
	"cmp"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/ryan/rflib/internal/domain"
	"github.com/ryan/rflib/internal/export"
	"github.com/ryan/rflib/internal/importer"
	"github.com/ryan/rflib/internal/store"
	"github.com/spf13/cobra"
)

// --- export ---

func newExportCommand() *cobra.Command {
	exp := &cobra.Command{Use: "export", Short: "Export library records"}

	csv := &cobra.Command{Use: "csv", Short: "Export channels as normalized generic CSV", Args: cobra.NoArgs}
	csv.Flags().String("output", "-", "Output path, or - for stdout")
	filterFields(csv)
	csv.RunE = func(cmd *cobra.Command, _ []string) error {
		return withStore(cmd, func(s *store.Store) error {
			cs, err := filteredChannels(cmd, s)
			if err != nil {
				return err
			}
			return writeOutput(cmd, len(cs), func(w io.Writer) error { return export.GenericCSV{}.Export(w, cs) })
		})
	}

	js := &cobra.Command{Use: "json", Short: "Export a full-fidelity JSON backup", Args: cobra.NoArgs,
		Long: "Export a JSON backup of channels and their groups, including IDs and timestamps.\nWithout filters every group is included, even empty ones."}
	js.Flags().String("output", "-", "Output path, or - for stdout")
	filterFields(js)
	js.RunE = func(cmd *cobra.Command, _ []string) error {
		return withStore(cmd, func(s *store.Store) error {
			cs, err := filteredChannels(cmd, s)
			if err != nil {
				return err
			}
			gs, err := s.ListGroups(ctx(cmd))
			if err != nil {
				return err
			}
			if filtered(cmd) {
				gs = referencedGroups(gs, cs)
			}
			return writeOutput(cmd, len(cs), func(w io.Writer) error { return export.WriteJSON(w, gs, cs) })
		})
	}

	exp.AddCommand(csv, js, newExportCHIRPCommand())
	return exp
}

func newExportCHIRPCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "chirp", Short: "Export channels as a CHIRP CSV file", Args: cobra.NoArgs,
		Long: `Export channels as a CSV file for CHIRP's File > Import.

Memories are numbered from --start in --sort order. CHIRP only accepts the
standard CTCSS and DCS tone sets and FM, NFM, and AM modes; channels outside
them fail the export unless --skip-invalid is given. A receive tone is only
programmed when tone squelch is on.`}
	cmd.Flags().String("output", "-", "Output path, or - for stdout")
	cmd.Flags().Int("start", 1, "First memory location")
	cmd.Flags().Int("max-name-len", 0, "Truncate names to this many characters (0 keeps full names)")
	cmd.Flags().String("sort", "name", "Memory order: name, freq, or group")
	cmd.Flags().Bool("skip-invalid", false, "Leave out channels CHIRP can't represent instead of failing")
	filterFields(cmd)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		start, _ := cmd.Flags().GetInt("start")
		maxLen, _ := cmd.Flags().GetInt("max-name-len")
		skipInvalid, _ := cmd.Flags().GetBool("skip-invalid")
		if start < 0 || maxLen < 0 {
			return fmt.Errorf("--start and --max-name-len must not be negative")
		}
		ex := export.CHIRP{Start: start, MaxNameLen: maxLen}
		return withStore(cmd, func(s *store.Store) error {
			cs, err := filteredChannels(cmd, s)
			if err != nil {
				return err
			}
			if err := sortChannels(cs, getString(cmd, "sort")); err != nil {
				return err
			}
			if skipInvalid {
				cs = validChannels(cmd.ErrOrStderr(), ex, cs)
			}
			return writeOutput(cmd, len(cs), func(w io.Writer) error { return ex.Export(w, cs) })
		})
	}
	return cmd
}

// sortChannels orders channels for exporters that number memories. The store
// already returns them by name, then frequency; sorts are stable on that.
func sortChannels(cs []domain.Channel, by string) error {
	switch by {
	case "name":
	case "freq":
		slices.SortStableFunc(cs, func(a, b domain.Channel) int { return cmp.Compare(a.ReceiveHz, b.ReceiveHz) })
	case "group":
		// Ungrouped channels sort last.
		key := func(c domain.Channel) string {
			if c.Group == nil {
				return "\uffff"
			}
			return c.Group.Name
		}
		slices.SortStableFunc(cs, func(a, b domain.Channel) int { return cmp.Compare(key(a), key(b)) })
	default:
		return fmt.Errorf("unknown sort %q; use name, freq, or group", by)
	}
	return nil
}

// validChannels drops channels the exporter can't represent, warning about
// each one.
func validChannels(warn io.Writer, v export.Validator, cs []domain.Channel) []domain.Channel {
	out := cs[:0]
	for _, c := range cs {
		if err := v.Validate(c); err != nil {
			fmt.Fprintf(warn, "skipped %q (%s MHz): %v\n", c.Name, domain.FormatFrequencyMHz(c.ReceiveHz), err)
			continue
		}
		out = append(out, c)
	}
	return out
}

func filteredChannels(cmd *cobra.Command, s *store.Store) ([]domain.Channel, error) {
	filter, err := channelFilter(cmd, s)
	if err != nil {
		return nil, err
	}
	return s.ListChannels(ctx(cmd), filter)
}

func filtered(cmd *cobra.Command) bool {
	for _, name := range []string{"group", "tag", "lat", "lon", "radius-km"} {
		if cmd.Flags().Changed(name) {
			return true
		}
	}
	return false
}

func referencedGroups(groups []domain.Group, channels []domain.Channel) []domain.Group {
	used := map[string]bool{}
	for _, c := range channels {
		if c.GroupID != nil {
			used[*c.GroupID] = true
		}
	}
	out := groups[:0]
	for _, g := range groups {
		if used[g.ID] {
			out = append(out, g)
		}
	}
	return out
}

// writeOutput writes to stdout or the --output file. A failed file export is
// removed so it can't be mistaken for a complete one. The summary goes to
// stderr so it never mixes with exported data on stdout.
func writeOutput(cmd *cobra.Command, count int, write func(io.Writer) error) error {
	path := getString(cmd, "output")
	if path == "" || path == "-" {
		return write(cmd.OutOrStdout())
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	err = write(file)
	if cerr := file.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(path)
		return err
	}
	fmt.Fprintln(cmd.ErrOrStderr(), "Exported", count, "channels to", path)
	return nil
}

// --- import ---

func newImportCommand() *cobra.Command {
	imp := &cobra.Command{Use: "import", Short: "Import channels from a file"}
	imp.AddCommand(
		newImportFormatCommand("csv", "Import the generic CSV format written by export csv", importer.ParseGenericCSV),
		newImportFormatCommand("json", "Restore a JSON backup written by export json", importer.ParseJSON),
		newImportFormatCommand("chirp", "Import a CHIRP CSV file (File > Export in CHIRP)", importer.ParseCHIRP),
	)
	return imp
}

func newImportFormatCommand(name, short string, parse func(io.Reader) (importer.Batch, error)) *cobra.Command {
	cmd := &cobra.Command{
		Use:   name + " [FILE]",
		Short: short,
		Long: short + `.

Reads FILE, or stdin when FILE is omitted or "-". The import is all-or-nothing:
any invalid row aborts it unless --skip-invalid is given.

A channel is a duplicate when it has the same ID as an existing channel, or the
same name, frequencies, tones, tone squelch, and group. Unknown groups are
created.`,
		Args: cobra.MaximumNArgs(1),
	}
	cmd.Flags().Bool("dry-run", false, "Report what would change without writing")
	cmd.Flags().String("on-duplicate", "skip", "What to do with duplicates: skip, update, or create")
	cmd.Flags().String("group", "", "Assign every imported channel to this existing group (ID or name)")
	cmd.Flags().String("tags", "", "Comma-separated tags added to every imported channel")
	cmd.Flags().Bool("skip-invalid", false, "Import valid rows even when some rows are invalid")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		policy, err := importer.ParseDuplicatePolicy(getString(cmd, "on-duplicate"))
		if err != nil {
			return err
		}
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		skipInvalid, _ := cmd.Flags().GetBool("skip-invalid")
		opts := importer.Options{OnDuplicate: policy, Group: getString(cmd, "group"), Tags: splitTags(getString(cmd, "tags")), DryRun: dryRun}

		return withStore(cmd, func(s *store.Store) error {
			batch, err := parseInput(cmd, args, parse)
			if err != nil {
				return err
			}
			for _, e := range batch.Errors {
				fmt.Fprintln(cmd.ErrOrStderr(), "invalid", e)
			}
			if len(batch.Errors) > 0 && !skipInvalid {
				return fmt.Errorf("%d invalid rows; nothing imported (fix them, or use --skip-invalid to import the rest)", len(batch.Errors))
			}
			sum, err := importer.Apply(ctx(cmd), s, batch, opts)
			if err != nil {
				return err
			}
			printSummary(cmd.OutOrStdout(), sum, len(batch.Errors), dryRun)
			return nil
		})
	}
	return cmd
}

func parseInput(cmd *cobra.Command, args []string, parse func(io.Reader) (importer.Batch, error)) (importer.Batch, error) {
	if len(args) == 0 || args[0] == "-" {
		return parse(cmd.InOrStdin())
	}
	f, err := os.Open(args[0])
	if err != nil {
		return importer.Batch{}, err
	}
	defer func() { _ = f.Close() }() // read-only; a close error loses nothing
	return parse(f)
}

func printSummary(out io.Writer, sum importer.Summary, invalid int, dryRun bool) {
	prefix := ""
	if dryRun {
		prefix = "Dry run, nothing written. Would have: "
	}
	fmt.Fprintf(out, "%screated %d, updated %d, skipped %d duplicate", prefix, sum.Created, sum.Updated, sum.Skipped)
	if sum.Skipped != 1 {
		fmt.Fprint(out, "s")
	}
	if invalid > 0 {
		fmt.Fprintf(out, ", skipped %d invalid", invalid)
	}
	fmt.Fprintln(out)
	if len(sum.GroupsCreated) > 0 {
		fmt.Fprintf(out, "New groups: %s\n", strings.Join(sum.GroupsCreated, ", "))
	}
}
