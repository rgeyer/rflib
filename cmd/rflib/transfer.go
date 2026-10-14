package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ryan/rflib/internal/app"
	"github.com/ryan/rflib/internal/domain"
	"github.com/ryan/rflib/internal/export"
	"github.com/ryan/rflib/internal/importer"
	"github.com/ryan/rflib/internal/naming"
	"github.com/ryan/rflib/internal/profile"
	"github.com/spf13/cobra"
)

// --- export ---

func newExportCommand() *cobra.Command {
	exp := &cobra.Command{Use: "export", Short: "Export library records"}

	csv := &cobra.Command{Use: "csv", Short: "Export channels as normalized generic CSV", Args: cobra.NoArgs}
	csv.Flags().String("output", "-", "Output path, or - for stdout")
	filterFields(csv)
	csv.RunE = func(cmd *cobra.Command, _ []string) error {
		return withApp(cmd, func(a *app.Service) error {
			cs, err := filteredChannels(cmd, a)
			if err != nil {
				return err
			}
			return writeOutput(cmd, func(w io.Writer) (int, error) { return len(cs), export.GenericCSV{}.Export(w, cs) })
		})
	}

	js := &cobra.Command{Use: "json", Short: "Export a full-fidelity JSON backup", Args: cobra.NoArgs,
		Long: "Export a JSON backup of channels, including IDs and timestamps."}
	js.Flags().String("output", "-", "Output path, or - for stdout")
	filterFields(js)
	js.RunE = func(cmd *cobra.Command, _ []string) error {
		return withApp(cmd, func(a *app.Service) error {
			cs, err := filteredChannels(cmd, a)
			if err != nil {
				return err
			}
			return writeOutput(cmd, func(w io.Writer) (int, error) { return len(cs), export.WriteJSON(w, cs) })
		})
	}

	exp.AddCommand(csv, js, newExportCHIRPCommand(), newExportProfileCommand())
	return exp
}

func newExportCHIRPCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "chirp", Short: "Export channels as a CHIRP CSV file", Args: cobra.NoArgs,
		Long: `Export channels as a CSV file for CHIRP's File > Import.

Memories are numbered from --start in --sort order. CHIRP only accepts the
standard CTCSS and DCS tone sets; channels outside them fail the export unless
--skip-invalid is given. A receive tone is only programmed when tone squelch
is on.

` + namesHelp}
	cmd.Flags().String("output", "-", "Output path, or - for stdout")
	cmd.Flags().Int("start", 1, "First memory location")
	cmd.Flags().Int("max-name-len", 0, "Fit names to this many characters (0 keeps full names)")
	cmd.Flags().String("sort", "name", "Memory order: name or freq")
	cmd.Flags().Bool("skip-invalid", false, "Leave out channels CHIRP can't represent instead of failing")
	dedupeFlag(cmd)
	filterFields(cmd)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		start, _ := cmd.Flags().GetInt("start")
		maxLen, _ := cmd.Flags().GetInt("max-name-len")
		if start < 0 || maxLen < 0 {
			return fmt.Errorf("--start and --max-name-len must not be negative")
		}
		return exportNumbered(cmd, export.CHIRP{Start: start, MaxNameLen: maxLen}, naming.Rules{MaxLen: maxLen})
	}
	return cmd
}

func newExportProfileCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "profile PROFILE", Short: "Export channels in a profile's format, such as a radio's programming software", Args: cobra.ExactArgs(1),
		Long: `Export channels in an export profile's format. PROFILE is a profile name
(see rflib profile list) or a .yaml file.

Memories are numbered from the profile's first location, or --start, in --sort
order. Channels the profile can't represent fail the export unless
--skip-invalid is given.

` + namesHelp}
	cmd.Flags().String("output", "-", "Output path, or - for stdout")
	cmd.Flags().Int("start", 1, "First memory location (default: the profile's)")
	cmd.Flags().String("sort", "name", "Memory order: name or freq")
	cmd.Flags().Bool("skip-invalid", false, "Leave out channels the profile can't represent instead of failing")
	dedupeFlag(cmd)
	filterFields(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		e, err := profile.Find(args[0])
		if err != nil {
			return err
		}
		ex := profile.Exporter{Profile: e.Profile}
		if cmd.Flags().Changed("start") {
			start, _ := cmd.Flags().GetInt("start")
			if start < 0 {
				return fmt.Errorf("--start must not be negative")
			}
			ex.Start = &start
		}
		return exportNumbered(cmd, ex, e.Profile.NameRules())
	}
	return cmd
}

const namesHelp = `Names are fitted to the radio's display: the full name if it fits, else the
longest of the channel's short names that fits, else a shortened name. Preview
them with "rflib names". --dedupe tells apart names that come out the same.`

func dedupeFlag(cmd *cobra.Command) {
	cmd.Flags().String("dedupe", "none", "Tell apart duplicate names: none, index (append 1, 2...), or freq (append frequency digits)")
}

// exportNumbered runs an export that numbers memories: filter, sort,
// optionally drop invalid channels, fit names, then write.
func exportNumbered(cmd *cobra.Command, ex interface {
	export.Exporter
	export.Validator
}, rules naming.Rules) error {
	skipInvalid, _ := cmd.Flags().GetBool("skip-invalid")
	dedupe, err := naming.ParseDedupe(getString(cmd, "dedupe"))
	if err != nil {
		return err
	}
	return withApp(cmd, func(a *app.Service) error {
		cs, err := filteredChannels(cmd, a)
		if err != nil {
			return err
		}
		if err := domain.SortChannels(cs, getString(cmd, "sort")); err != nil {
			return err
		}
		if skipInvalid {
			cs = validChannels(cmd.ErrOrStderr(), ex, cs)
		}
		cs, results := naming.Apply(cs, rules, dedupe)
		warnDuplicateNames(cmd, results)
		return writeOutput(cmd, func(w io.Writer) (int, error) { return len(cs), ex.Export(w, cs) })
	})
}

func warnDuplicateNames(cmd *cobra.Command, results []naming.Result) {
	if groups := naming.Duplicates(results); len(groups) > 0 {
		names := make([]string, len(groups))
		for i, g := range groups {
			names[i] = fmt.Sprintf("%q (%d)", results[g[0]].Name, len(g))
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: duplicate names on the radio: %s; use --dedupe index or freq to tell them apart\n", strings.Join(names, ", "))
	}
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

// writeOutput writes to stdout or the --output file. A failed file export is
// removed so it can't be mistaken for a complete one. The summary goes to
// stderr so it never mixes with exported data on stdout.
// writeOutput runs write to --output, or stdout, and reports on stderr how
// many channels write said it wrote to a file.
func writeOutput(cmd *cobra.Command, write func(io.Writer) (int, error)) error {
	path := getString(cmd, "output")
	if path == "" || path == "-" {
		_, err := write(cmd.OutOrStdout())
		return err
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	count, err := write(file)
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
		newImportProfileCommand(),
	)
	return imp
}

func newImportFormatCommand(name, short string, parse func(io.Reader) (importer.Batch, error)) *cobra.Command {
	cmd := &cobra.Command{Use: name + " [FILE]", Short: short, Long: short + ".\n\n" + importHelp, Args: cobra.MaximumNArgs(1)}
	fileImportFlags(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error { return runFileImport(cmd, args, parse) }
	return cmd
}

func newImportProfileCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "profile PROFILE [FILE]", Short: "Import a file in an export profile's format",
		Long: "Import a file in an export profile's format, such as one saved by a radio's\nprogramming software. PROFILE is a profile name (see rflib profile list) or\na .yaml file.\n\n" + importHelp,
		Args: cobra.RangeArgs(1, 2)}
	fileImportFlags(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		e, err := profile.Find(args[0])
		if err != nil {
			return err
		}
		return runFileImport(cmd, args[1:], profile.Parser{Profile: e.Profile}.Parse)
	}
	return cmd
}

const importHelp = `Reads FILE, or stdin when FILE is omitted or "-". The import is all-or-nothing:
any invalid row aborts it unless --skip-invalid is given.

A channel is a duplicate when it has the same ID as an existing channel, or the
same name, frequencies, tones, and tone squelch. Files written before groups
were removed bring each channel's group in as a tag, and its location.`

func fileImportFlags(cmd *cobra.Command) {
	importFlags(cmd)
	cmd.Flags().String("service", "", "Set every imported channel's service (noaa and public_safety also default to --rx-only)")
	cmd.Flags().Bool("rx-only", false, "Set every imported channel's receive-only flag")
	cmd.Flags().Bool("skip-invalid", false, "Import valid rows even when some rows are invalid")
}

func runFileImport(cmd *cobra.Command, args []string, parse func(io.Reader) (importer.Batch, error)) error {
	opts, err := importOptions(cmd)
	if err != nil {
		return err
	}
	if opts.Service, err = domain.ParseService(getString(cmd, "service")); err != nil {
		return err
	}
	if cmd.Flags().Changed("rx-only") {
		v, _ := cmd.Flags().GetBool("rx-only")
		opts.RXOnly = &v
	}
	skipInvalid, _ := cmd.Flags().GetBool("skip-invalid")
	return withApp(cmd, func(a *app.Service) error {
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
		return applyImport(cmd, a, batch, opts, len(batch.Errors))
	})
}

// importFlags adds the flags shared by every command that imports channels.
func importFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("dry-run", false, "Report what would change without writing")
	cmd.Flags().String("on-duplicate", "skip", "What to do with duplicates: skip, update, or create")
	cmd.Flags().String("match", "exact", "What makes a duplicate: exact (name, frequencies, and tones) or programmed (frequencies and tones only)")
	cmd.Flags().String("tags", "", "Comma-separated tags added to every imported channel")
}

func importOptions(cmd *cobra.Command) (importer.Options, error) {
	policy, err := importer.ParseDuplicatePolicy(getString(cmd, "on-duplicate"))
	if err != nil {
		return importer.Options{}, err
	}
	match, err := importer.ParseMatchRule(getString(cmd, "match"))
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	return importer.Options{OnDuplicate: policy, Match: match, Tags: splitTags(getString(cmd, "tags")), DryRun: dryRun}, err
}

func applyImport(cmd *cobra.Command, a *app.Service, batch importer.Batch, opts importer.Options, invalid int) error {
	sum, err := a.Import(ctx(cmd), batch, opts)
	if err != nil {
		return err
	}
	for _, w := range sum.Warnings {
		fmt.Fprintln(cmd.ErrOrStderr(), "warning", w)
	}
	printSummary(cmd.ErrOrStderr(), sum, invalid, opts.DryRun)
	return nil
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
	if len(sum.Warnings) > 0 {
		fmt.Fprintf(out, "%d band plan warnings (see above; rflib check lists them again)\n", len(sum.Warnings))
	}
}
