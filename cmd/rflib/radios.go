package main

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/ryan/rflib/internal/app"
	"github.com/ryan/rflib/internal/domain"
	"github.com/ryan/rflib/internal/model"
	"github.com/ryan/rflib/internal/program"
	"github.com/spf13/cobra"
)

// --- models ---

func newModelCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "model", Short: "List the radio models rflib can program"}
	list := &cobra.Command{Use: "list", Short: "List radio models", Args: cobra.NoArgs}
	modelListing.flags(list)
	list.RunE = func(cmd *cobra.Command, _ []string) error {
		all, err := model.All()
		if err != nil {
			return err
		}
		return modelListing.write(cmd, all)
	}
	show := &cobra.Command{Use: "show MODEL", Short: "Show a radio model's facts and where they came from", Args: cobra.ExactArgs(1)}
	show.RunE = func(cmd *cobra.Command, args []string) error {
		if _, err := model.Find(args[0]); err != nil {
			return err
		}
		data, err := model.YAML(args[0])
		if err != nil {
			return err
		}
		_, err = cmd.OutOrStdout().Write(data)
		return err
	}
	cmd.AddCommand(list, show)
	return cmd
}

var modelListing = listing[model.Model]{
	fields: []field[model.Model]{
		{name: "name", text: func(m model.Model) string { return m.Name }},
		{name: "description", text: func(m model.Model) string { return m.Description }},
		{name: "format", text: formatName},
		{name: "memories", text: func(m model.Model) string { return fmt.Sprintf("%d-%d", m.Memories.First, m.Memories.Last) }},
		{name: "name_length", text: func(m model.Model) string { return strconv.Itoa(m.Names.MaxLength) }, json: func(m model.Model) any { return m.Names.MaxLength }},
		{name: "modes", text: func(m model.Model) string { return strings.Join(m.Modes, ",") }, json: func(m model.Model) any { return nonNil(m.Modes) }},
	},
	defaults: []string{"name", "description", "format"},
}

func formatName(m model.Model) string {
	if p, ok := m.ProfileName(); ok {
		return "profile " + p
	}
	return "CHIRP: " + m.ChirpModel
}

// --- radios ---

func newRadioCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "radio", Short: "Manage your radios and the channels programmed into each",
		Long: `A radio is one of your own radios: a model (see rflib model list), an ordered
list of channels, and per-radio choices. Memory numbers follow the list order
from the model's first memory; a channel can start a section at a chosen
memory number, and the channels after it continue from there.

Power: each channel's power comes from the radio's override for it, else the
channel's power, else the radio's default, mapped to the model's levels and
held to FRS, GMRS, MURS, and CB limits.

Sources: a radio can keep searches (rflib radio sources) that radio sync uses
to add new matching channels, each in its own part of the list.`}
	cmd.AddCommand(newRadioAddCommand(), newRadioListCommand(), newRadioShowCommand(), newRadioUpdateCommand(), newRadioDeleteCommand(),
		newRadioChannelsCommand(), newRadioSourcesCommand(), newRadioSyncCommand(), newRadioExportCommand())
	return cmd
}

func radioFields(cmd *cobra.Command) {
	cmd.Flags().String("model", "", "Radio model (see rflib model list)")
	cmd.Flags().String("default-power", "", "Power for channels without their own: low, medium, high, or max")
	cmd.Flags().String("dedupe", "", "Tell apart duplicate names: none, index, or freq")
	cmd.Flags().String("notes", "", "Notes")
}

func applyRadioFlags(cmd *cobra.Command, r *domain.Radio, all bool) error {
	set := func(name string) bool { return all || cmd.Flags().Changed(name) }
	if set("model") {
		m, err := model.Find(getString(cmd, "model"))
		if err != nil {
			return err
		}
		r.Model = m.Name
	}
	if set("default-power") {
		p, err := domain.ParsePowerIntent(getString(cmd, "default-power"))
		if err != nil {
			return err
		}
		r.DefaultPower = p
	}
	if set("dedupe") {
		r.Dedupe = getString(cmd, "dedupe")
	}
	if set("notes") {
		r.Notes = getString(cmd, "notes")
	}
	return nil
}

func newRadioAddCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "add NAME", Short: "Add one of your radios", Args: cobra.ExactArgs(1)}
	radioFields(cmd)
	_ = cmd.MarkFlagRequired("model")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		r := domain.Radio{Name: args[0]}
		if err := applyRadioFlags(cmd, &r, true); err != nil {
			return err
		}
		return withApp(cmd, func(a *app.Service) error {
			r, err := a.CreateRadio(ctx(cmd), r)
			if err == nil {
				fmt.Fprintln(cmd.OutOrStdout(), r.ID)
			}
			return err
		})
	}
	return cmd
}

func newRadioListCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "list", Short: "List your radios", Args: cobra.NoArgs}
	radioListing.flags(cmd)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if err := radioListing.check(cmd); err != nil {
			return err
		}
		return withApp(cmd, func(a *app.Service) error {
			rs, err := a.ListRadios(ctx(cmd))
			if err != nil {
				return err
			}
			rows := make([]radioRow, len(rs))
			for i, r := range rs {
				_, es, err := a.RadioEntries(ctx(cmd), r.ID)
				if err != nil {
					return err
				}
				rows[i] = radioRow{r, len(es)}
			}
			return radioListing.write(cmd, rows)
		})
	}
	return cmd
}

type radioRow struct {
	domain.Radio
	channels int
}

var radioListing = listing[radioRow]{
	fields: []field[radioRow]{
		{name: "name", text: func(r radioRow) string { return r.Name }},
		{name: "model", text: func(r radioRow) string { return r.Model }},
		{name: "channels", text: func(r radioRow) string { return strconv.Itoa(r.channels) }, json: func(r radioRow) any { return r.channels }},
		{name: "default_power", text: func(r radioRow) string { return string(r.DefaultPower) }},
		{name: "dedupe", text: func(r radioRow) string { return orDefault(r.Dedupe, "none") }},
		{name: "notes", text: func(r radioRow) string { return r.Notes }},
		{name: "id", id: true, text: func(r radioRow) string { return r.ID }},
	},
	defaults: []string{"name", "model", "channels", "default_power"},
}

func newRadioShowCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "show RADIO", Short: "Show a radio's settings", Args: cobra.ExactArgs(1)}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return withApp(cmd, func(a *app.Service) error {
			r, sources, es, err := a.Sources(ctx(cmd), args[0])
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "%s\nname: %s\nmodel: %s\nchannels: %d\ndefault power: %s\ndedupe: %s\nnotes: %s\nsources:",
				r.ID, r.Name, r.Model, len(es), powerName(r.DefaultPower), orDefault(r.Dedupe, "none"), r.Notes)
			if len(sources) == 0 {
				fmt.Fprint(out, " none")
			}
			fmt.Fprintln(out)
			return sourceListing.write(cmd, sourceRows(sources, es))
		})
	}
	return cmd
}

func orDefault(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

func newRadioUpdateCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "update RADIO", Short: "Change a radio's settings; only supplied flags change", Args: cobra.ExactArgs(1)}
	radioFields(cmd)
	cmd.Flags().String("name", "", "New name")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return withApp(cmd, func(a *app.Service) error {
			r, err := a.GetRadio(ctx(cmd), args[0])
			if err != nil {
				return err
			}
			if cmd.Flags().Changed("name") {
				r.Name = getString(cmd, "name")
			}
			if err := applyRadioFlags(cmd, &r, false); err != nil {
				return err
			}
			_, err = a.UpdateRadio(ctx(cmd), r)
			return err
		})
	}
	return cmd
}

func newRadioDeleteCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "delete RADIO", Short: "Delete a radio; its channels stay in the library", Args: cobra.ExactArgs(1)}
	cmd.Flags().Bool("yes", false, "Confirm deletion")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := requireYes(cmd, "radio"); err != nil {
			return err
		}
		return withApp(cmd, func(a *app.Service) error { return a.DeleteRadio(ctx(cmd), args[0]) })
	}
	return cmd
}

// --- a radio's channels ---

func newRadioChannelsCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "channels", Short: "List and arrange the channels on a radio"}
	cmd.AddCommand(newRadioChannelsListCommand(), newRadioChannelsAddCommand(), newRadioChannelsRemoveCommand(),
		newRadioChannelsMoveCommand(), newRadioChannelsSetCommand())
	return cmd
}

func printWarnings(w io.Writer, p program.Layout) {
	for _, msg := range p.Warnings {
		fmt.Fprintln(w, "warning:", msg)
	}
}

func newRadioChannelsListCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "list RADIO", Short: "Show each memory as it will be programmed", Args: cobra.ExactArgs(1),
		Long: `Show each memory as it will be programmed: number, name on the radio and how
it was fitted, power level, scan, frequency, tone, and the library channel
when its name differs. "§" marks a section start; "*" marks a value the radio
overrides. Warnings go to stderr.

` + tableHelp}
	memoryListing.flags(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := memoryListing.check(cmd); err != nil {
			return err
		}
		return withApp(cmd, func(a *app.Service) error {
			p, es, err := a.Layout(ctx(cmd), args[0])
			if err != nil {
				return err
			}
			rows := make([]memory, len(p.Memories))
			for i, mem := range p.Memories {
				rows[i] = memory{mem, es[i]}
			}
			if err := memoryListing.write(cmd, rows); err != nil {
				return err
			}
			printWarnings(cmd.ErrOrStderr(), p)
			return nil
		})
	}
	return cmd
}

func mark(on bool, m string) string {
	if on {
		return m
	}
	return ""
}

func scanText(skip bool) string {
	if skip {
		return "skip"
	}
	return "scan"
}

func powerLabel(mem program.Memory) string {
	if mem.Channel.ProgrammedPower == nil {
		return "-"
	}
	return mem.Channel.ProgrammedPower.Label
}

func fitFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("any-band", false, "Add channels even when their receive frequency is outside the radio's bands")
	cmd.Flags().Bool("any-mode", false, "Add channels even when the radio doesn't have their mode, such as USB")
}

func skippedNote(res app.AddResult) string {
	var parts []string
	if n := len(res.Skipped); n > 0 {
		parts = append(parts, fmt.Sprintf("%d outside its bands", n))
	}
	if n := len(res.WrongMode); n > 0 {
		parts = append(parts, fmt.Sprintf("%d in modes it doesn't have", n))
	}
	if len(parts) == 0 {
		return ""
	}
	return "; skipped " + strings.Join(parts, " and ")
}

func newRadioChannelsAddCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "add RADIO", Short: "Append channels to the end of a radio's list", Args: cobra.ExactArgs(1),
		Long: `Append channels to the end of a radio's list, by --id or by --query (or the
shorthand --tag, --service, --lat/--lon/--radius-km), in --sort
order. Channels already on the radio are skipped, so the existing order never
changes, and so are channels outside the radio's bands unless --any-band, and
channels in a mode the radio doesn't have (such as USB) unless --any-mode. To
keep adding a query's new matches, make it a source instead: rflib radio
sources add. With both --id and a query, a channel must match both.

` + stdinIDHelp}
	idsFlag(cmd, "Channel IDs, added in the order given")
	cmd.Flags().String("sort", "name", "Order for channels added by query: name or freq")
	fitFlags(cmd)
	filterFields(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		var req app.AddRequest
		var err error
		if req.IDs, err = getIDs(cmd); err != nil {
			return err
		}
		req.Sort = getString(cmd, "sort")
		req.AnyBand, _ = cmd.Flags().GetBool("any-band")
		req.AnyMode, _ = cmd.Flags().GetBool("any-mode")
		if len(req.IDs) == 0 && !filtered(cmd) {
			return fmt.Errorf("give channels with --id or --query")
		}
		if filtered(cmd) {
			n, err := channelQuery(cmd, nil)
			if err != nil {
				return err
			}
			req.Query = n
		}
		return withApp(cmd, func(a *app.Service) error {
			r, res, err := a.AddChannels(ctx(cmd), args[0], req)
			if err != nil {
				return err
			}
			for _, c := range res.Skipped {
				fmt.Fprintf(cmd.ErrOrStderr(), "skipped %q (%s MHz): %s can't receive it\n", c.Name, domain.FormatFrequencyMHz(c.ReceiveHz), r.Name)
			}
			for _, c := range res.WrongMode {
				fmt.Fprintf(cmd.ErrOrStderr(), "skipped %q (%s MHz): %s doesn't have %s\n", c.Name, domain.FormatFrequencyMHz(c.ReceiveHz), r.Name, c.Mode)
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "Added %d channels to %s%s\n", res.Added, r.Name, skippedNote(res))
			return nil
		})
	}
	return cmd
}

func newRadioChannelsRemoveCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "remove RADIO", Short: "Take channels off a radio (they stay in the library)", Args: cobra.ExactArgs(1),
		Long: `Take channels off a radio by --id, by query, or --all; they stay in the
library, and the list closes up. Removing by query or --all needs --yes;
--dry-run lists what would come off.

` + stdinIDHelp}
	idsFlag(cmd, "Channel IDs")
	filterFields(cmd)
	cmd.Flags().Bool("all", false, "Take every channel off")
	cmd.Flags().Bool("dry-run", false, "List the channels that would come off")
	cmd.Flags().Bool("yes", false, "Confirm removing by query or --all")
	channelListing.flags(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		sel, err := selection(cmd)
		if err != nil {
			return err
		}
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		if yes, _ := cmd.Flags().GetBool("yes"); !dryRun && !yes && (sel.All || sel.Query != nil) {
			return fmt.Errorf("removing channels by query or --all requires --yes (or --dry-run to see which)")
		}
		return withApp(cmd, func(a *app.Service) error {
			if dryRun {
				r, cs, err := a.OnRadio(ctx(cmd), args[0], sel)
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "Would remove %d channels from %s\n", len(cs), r.Name)
				return channelListing.write(cmd, cs)
			}
			r, cs, err := a.RemoveChannels(ctx(cmd), args[0], sel)
			if err == nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "Removed %d channels from %s\n", len(cs), r.Name)
			}
			return err
		})
	}
	return cmd
}

func newRadioChannelsMoveCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "move RADIO", Short: "Move a channel within a radio's list", Args: cobra.ExactArgs(1)}
	cmd.Flags().String("id", "", "Channel ID to move, or - to read it from stdin (required)")
	cmd.Flags().String("before", "", "Move just before this channel ID")
	cmd.Flags().String("after", "", "Move just after this channel ID")
	cmd.Flags().Int("to", 0, "Move to this position in the list, from 1")
	cmd.Flags().Bool("top", false, "Move to the top")
	cmd.Flags().Bool("bottom", false, "Move to the bottom")
	cmd.MarkFlagsMutuallyExclusive("before", "after", "to", "top", "bottom")
	cmd.MarkFlagsOneRequired("before", "after", "to", "top", "bottom")
	_ = cmd.MarkFlagRequired("id")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		mv := app.Move{Before: getString(cmd, "before"), After: getString(cmd, "after")}
		mv.Top, _ = cmd.Flags().GetBool("top")
		mv.Bottom, _ = cmd.Flags().GetBool("bottom")
		if cmd.Flags().Changed("to") {
			mv.To, _ = cmd.Flags().GetInt("to")
			if mv.To < 1 {
				return fmt.Errorf("--to counts from 1")
			}
		}
		id, err := getID(cmd)
		if err != nil {
			return err
		}
		return withApp(cmd, func(a *app.Service) error {
			return a.MoveChannel(ctx(cmd), args[0], id, mv)
		})
	}
	return cmd
}

func newRadioChannelsSetCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "set RADIO", Short: "Set a radio's overrides for its channels", Args: cobra.ExactArgs(1),
		Long: `Set a radio's overrides for channels on it, chosen by --id, by query, or both.
Only supplied flags change; an empty value removes that override, so the
channel's own setting applies. Many channels at once (a query, or several
IDs) can take --power and --scan; --name and --section-start are set one
channel at a time.

` + stdinIDHelp}
	idsFlag(cmd, "Channel IDs")
	filterFields(cmd)
	cmd.Flags().String("name", "", "Name on this radio")
	cmd.Flags().String("power", "", "Power on this radio: low, medium, high, or max")
	cmd.Flags().String("scan", "", "Scan on this radio: yes, no, or default (follow the channel)")
	cmd.Flags().String("section-start", "", "Start a section at this memory number (empty ends it)")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		sel, err := selection(cmd)
		if err != nil {
			return err
		}
		var edit app.EntryEdit
		if edit.SetPower = cmd.Flags().Changed("power"); edit.SetPower {
			if edit.Power, err = domain.ParsePowerIntent(getString(cmd, "power")); err != nil {
				return err
			}
		}
		if edit.SetSkip = cmd.Flags().Changed("scan"); edit.SetSkip {
			switch v := strings.ToLower(getString(cmd, "scan")); v {
			case "yes", "no":
				skip := v == "no"
				edit.Skip = &skip
			case "default", "":
			default:
				return fmt.Errorf("--scan must be yes, no, or default")
			}
		}
		one := cmd.Flags().Changed("name") || cmd.Flags().Changed("section-start")
		if one && sel.Bulk() {
			return fmt.Errorf("--name and --section-start are set one channel at a time; give a single --id")
		}
		if !one && !edit.SetPower && !edit.SetSkip {
			return fmt.Errorf("nothing to set; give --name, --power, --scan, or --section-start")
		}
		return withApp(cmd, func(a *app.Service) error {
			if sel.Bulk() {
				cs, err := a.SetEntries(ctx(cmd), args[0], sel, edit)
				if cs != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "Set overrides on %d channels\n", len(cs))
				}
				return err
			}
			r, e, err := a.RadioEntry(ctx(cmd), args[0], sel.IDs[0])
			if err != nil {
				return err
			}
			if cmd.Flags().Changed("name") {
				e.Name = getString(cmd, "name")
			}
			if edit.SetPower {
				e.Power = edit.Power
			}
			if edit.SetSkip {
				e.Skip = edit.Skip
			}
			if cmd.Flags().Changed("section-start") {
				if e.SectionStart, err = parseSectionStart(getString(cmd, "section-start")); err != nil {
					return err
				}
			}
			return a.SetEntry(ctx(cmd), r.ID, e)
		})
	}
	return cmd
}

func newRadioSyncCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "sync RADIO", Short: "Add new channels matching the radio's sources", Args: cobra.ExactArgs(1),
		Long: `Add channels matching each of the radio's sources that aren't on it yet.
Each source's new channels go after the last channel it added, in the
source's sort order; an earlier source claims a channel first. Channels
outside the radio's bands are skipped unless --any-band, and channels in a mode
the radio doesn't have unless --any-mode. Nothing is removed.

Channels already on the radio stay where they are, except in sources set to
--keep-sorted, or in every source with --resort. Re-sorting puts a source's
channels in its sort order within the places they hold in the list, so
memory numbers change and the radio needs programming again. Channels added
or moved by hand keep their places, and section starts stay put.`}
	cmd.Flags().Bool("dry-run", false, "List what would change without changing the radio")
	cmd.Flags().Bool("resort", false, "Also re-sort every source's channels already on the radio")
	fitFlags(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		var opts app.SyncOptions
		opts.DryRun, _ = cmd.Flags().GetBool("dry-run")
		opts.AnyBand, _ = cmd.Flags().GetBool("any-band")
		opts.AnyMode, _ = cmd.Flags().GetBool("any-mode")
		opts.Resort, _ = cmd.Flags().GetBool("resort")
		dryRun := opts.DryRun
		return withApp(cmd, func(a *app.Service) error {
			res, err := a.Sync(ctx(cmd), args[0], opts)
			if err != nil {
				return err
			}
			out, status := cmd.OutOrStdout(), cmd.ErrOrStderr()
			for _, ss := range res.Sources {
				n := ss.Source.Position + 1
				if dryRun {
					for _, c := range ss.Add {
						fmt.Fprintf(out, "would add\tsource %d\t%s\t%s\n", n, c.Name, domain.FormatFrequencyMHz(c.ReceiveHz))
					}
					for _, c := range ss.Skipped {
						fmt.Fprintf(out, "would skip\tsource %d\t%s\t%s\toutside the radio's bands\n", n, c.Name, domain.FormatFrequencyMHz(c.ReceiveHz))
					}
					for _, c := range ss.WrongMode {
						fmt.Fprintf(out, "would skip\tsource %d\t%s\t%s\tthe radio doesn't have %s\n", n, c.Name, domain.FormatFrequencyMHz(c.ReceiveHz), c.Mode)
					}
					for _, c := range ss.Removed {
						fmt.Fprintf(out, "would remove\tsource %d\t%s\t%s\tno longer in the plan\n", n, c.Name, domain.FormatFrequencyMHz(c.ReceiveHz))
					}
					if ss.Linked > 0 {
						fmt.Fprintf(out, "would link\tsource %d\t%d shared channels\n", n, ss.Linked)
					}
					if ss.Resorted > 0 {
						fmt.Fprintf(out, "would re-sort\tsource %d\t%d channels\n", n, ss.Resorted)
					}
					if ss.Note != "" {
						fmt.Fprintf(out, "note\tsource %d\t%s\n", n, ss.Note)
					}
					continue
				}
				if ss.Note != "" {
					fmt.Fprintf(status, "%s: %s\n", app.SourceLabel(ss.Source), ss.Note)
				}
				if ss.Linked > 0 {
					fmt.Fprintf(status, "%s: linked %d shared channels into the library\n", app.SourceLabel(ss.Source), ss.Linked)
				}
				if len(ss.Removed) > 0 {
					fmt.Fprintf(status, "%s: removed %d no longer in the plan\n", app.SourceLabel(ss.Source), len(ss.Removed))
				}
				if len(ss.Add) > 0 || len(ss.Skipped) > 0 || len(ss.WrongMode) > 0 {
					fmt.Fprintf(status, "%s: added %d%s\n", app.SourceLabel(ss.Source), ss.Added, skippedNote(ss.AddResult))
				}
				if ss.Resorted > 0 {
					fmt.Fprintf(status, "%s: re-sorted %d\n", app.SourceLabel(ss.Source), ss.Resorted)
				}
			}
			if dryRun {
				return nil
			}
			fmt.Fprintf(status, "Added %d channels to %s\n", res.Added(), res.Radio.Name)
			if n := res.Resorted(); n > 0 {
				fmt.Fprintf(status, "Re-sorted %d channels; program the radio again\n", n)
			}
			if res.LayoutErr != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s no longer exports: %v\n", res.Radio.Name, res.LayoutErr)
			}
			return nil
		})
	}
	return cmd
}

// --- a radio's sources ---

func newRadioSourcesCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "sources", Short: "Manage the searches radio sync adds channels from",
		Long: `A radio's sources are searches that radio sync adds new matching channels
from. Each source keeps its channels together in the radio's list, in its own
sort order, and can start a section at a memory number. Sources are numbered
from 1 in the order radio sources list shows.

` + queryHelp}
	cmd.AddCommand(newRadioSourcesListCommand(), newRadioSourcesAddCommand(), newRadioSourcesSetCommand(),
		newRadioSourcesMoveCommand(), newRadioSourcesRemoveCommand())
	return cmd
}

type sourceRow struct {
	domain.RadioSource
	channels int
}

func sourceRows(sources []domain.RadioSource, es []domain.RadioEntry) []sourceRow {
	count := map[string]int{}
	for _, e := range es {
		count[e.SourceID]++
	}
	rows := make([]sourceRow, len(sources))
	for i, src := range sources {
		rows[i] = sourceRow{src, count[src.ID]}
	}
	return rows
}

var sourceListing = listing[sourceRow]{
	fields: []field[sourceRow]{
		{name: "number", text: func(r sourceRow) string { return strconv.Itoa(r.Position + 1) }, json: func(r sourceRow) any { return r.Position + 1 }},
		{name: "section", text: func(r sourceRow) string {
			if r.SectionStart == nil {
				return ""
			}
			return strconv.Itoa(*r.SectionStart)
		}, json: func(r sourceRow) any { return r.SectionStart }},
		{name: "sort", text: func(r sourceRow) string { return r.Sort }},
		{name: "kept_sorted", text: func(r sourceRow) string { return yesNo(r.KeepSorted) }, json: func(r sourceRow) any { return r.KeepSorted }},
		{name: "channels", text: func(r sourceRow) string { return strconv.Itoa(r.channels) }, json: func(r sourceRow) any { return r.channels }},
		{name: "query", text: func(r sourceRow) string { return r.Query }},
		{name: "plan", text: func(r sourceRow) string { return r.PlanName }},
		{name: "id", id: true, text: func(r sourceRow) string { return r.ID }},
	},
	defaults: []string{"number", "section", "sort", "kept_sorted", "channels", "query", "plan"},
}

func keepSortedFlag(cmd *cobra.Command) {
	cmd.Flags().Bool("keep-sorted", false, "Re-sort the source's channels on every radio sync (--keep-sorted=false stops)")
}

// sourceNumber parses a source number argument.
func sourceNumber(v string) (int, error) {
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%q is not a source number; see rflib radio sources list", v)
	}
	return n, nil
}

func parseSectionStart(v string) (*int, error) {
	if strings.TrimSpace(v) == "" {
		return nil, nil
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return nil, fmt.Errorf("--section-start must be a memory number")
	}
	return &n, nil
}

func newRadioSourcesListCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "list RADIO", Short: "List a radio's sources", Args: cobra.ExactArgs(1)}
	sourceListing.flags(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := sourceListing.check(cmd); err != nil {
			return err
		}
		return withApp(cmd, func(a *app.Service) error {
			_, sources, es, err := a.Sources(ctx(cmd), args[0])
			if err != nil {
				return err
			}
			return sourceListing.write(cmd, sourceRows(sources, es))
		})
	}
	return cmd
}

func newRadioSourcesAddCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "add RADIO [QUERY...]", Short: "Add a source to the end of a radio's sources", Args: cobra.MinimumNArgs(1),
		Long: `Add a source to the end of a radio's sources: a query, or with --plan a
communications plan from the shared list (see rflib plan). A plan source
follows the plan: radio sync links its channels into the library, puts them
in the plan's order with the plan's names, and numbers them as the plan does
from the section start. Removing the source takes its channels off.`}
	cmd.Flags().String("sort", "name", "Order its channels are added in: name or freq")
	cmd.Flags().String("section-start", "", "Start the source's channels at this memory number")
	cmd.Flags().String("plan", "", "Use a plan, by name, instead of a query")
	keepSortedFlag(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		section, err := parseSectionStart(getString(cmd, "section-start"))
		if err != nil {
			return err
		}
		if (len(args) > 1) == (getString(cmd, "plan") != "") {
			return fmt.Errorf("give a query or --plan, not both")
		}
		keep, _ := cmd.Flags().GetBool("keep-sorted")
		src := domain.RadioSource{Query: strings.Join(args[1:], " "), Sort: getString(cmd, "sort"), SectionStart: section, KeepSorted: keep, PlanID: getString(cmd, "plan")}
		return withApp(cmd, func(a *app.Service) error {
			r, src, err := a.AddSource(ctx(cmd), args[0], src)
			if err == nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "Added source %d to %s; run rflib radio sync %s to add its channels\n", src.Position+1, r.Name, r.Name)
			}
			return err
		})
	}
	return cmd
}

func newRadioSourcesSetCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "set RADIO N", Short: "Change a source; only supplied flags change", Args: cobra.ExactArgs(2),
		Long: `Change a source; only supplied flags change. Channels it already added stay;
radio sync adds the new matches. A new section start moves to the source's
first channel now.`}
	cmd.Flags().String("query", "", "New query")
	cmd.Flags().String("sort", "", "Order its channels are added in: name or freq")
	cmd.Flags().String("section-start", "", "Start the source's channels at this memory number (empty ends it)")
	keepSortedFlag(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		n, err := sourceNumber(args[1])
		if err != nil {
			return err
		}
		return withApp(cmd, func(a *app.Service) error {
			_, src, err := a.Source(ctx(cmd), args[0], n)
			if err != nil {
				return err
			}
			if cmd.Flags().Changed("query") {
				src.Query = getString(cmd, "query")
			}
			if cmd.Flags().Changed("sort") {
				src.Sort = getString(cmd, "sort")
			}
			if cmd.Flags().Changed("section-start") {
				if src.SectionStart, err = parseSectionStart(getString(cmd, "section-start")); err != nil {
					return err
				}
			}
			if cmd.Flags().Changed("keep-sorted") {
				src.KeepSorted, _ = cmd.Flags().GetBool("keep-sorted")
			}
			return a.UpdateSource(ctx(cmd), src)
		})
	}
	return cmd
}

func newRadioSourcesMoveCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "move RADIO N", Short: "Move a source; earlier sources claim a channel first", Args: cobra.ExactArgs(2)}
	cmd.Flags().Int("to", 0, "New number for the source, from 1 (required)")
	_ = cmd.MarkFlagRequired("to")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		to, _ := cmd.Flags().GetInt("to")
		if to < 1 {
			return fmt.Errorf("--to counts from 1")
		}
		n, err := sourceNumber(args[1])
		if err != nil {
			return err
		}
		return withApp(cmd, func(a *app.Service) error { return a.MoveSource(ctx(cmd), args[0], n, to) })
	}
	return cmd
}

func newRadioSourcesRemoveCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "remove RADIO N", Short: "Remove a source; its channels stay unless --with-channels or it's a plan", Args: cobra.ExactArgs(2)}
	cmd.Flags().Bool("with-channels", false, "Also take the channels it added off the radio")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		withChannels, _ := cmd.Flags().GetBool("with-channels")
		n, err := sourceNumber(args[1])
		if err != nil {
			return err
		}
		return withApp(cmd, func(a *app.Service) error {
			r, err := a.GetRadio(ctx(cmd), args[0])
			if err != nil {
				return err
			}
			removed, err := a.RemoveSource(ctx(cmd), r.ID, n, withChannels)
			if err == nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "Removed source %d from %s", n, r.Name)
				if withChannels || removed > 0 {
					fmt.Fprintf(cmd.ErrOrStderr(), " and %d channels", removed)
				}
				fmt.Fprintln(cmd.ErrOrStderr())
			}
			return err
		})
	}
	return cmd
}

func newRadioExportCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "export RADIO", Short: "Export a radio's programming file", Args: cobra.ExactArgs(1),
		Long: `Export a radio's programming file in its model's format: a CHIRP CSV for CHIRP
radios (open the radio's image in CHIRP, then File > Import), or the
manufacturer software's CSV for profile radios. Warnings go to stderr.`}
	cmd.Flags().String("output", "-", "Output path, or - for stdout")
	cmd.Flags().Bool("skip-invalid", false, "Leave out memories the format can't represent instead of failing")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		skipInvalid, _ := cmd.Flags().GetBool("skip-invalid")
		return withApp(cmd, func(a *app.Service) error {
			p, _, err := a.Layout(ctx(cmd), args[0])
			if err != nil {
				return err
			}
			printWarnings(cmd.ErrOrStderr(), p)
			return writeOutput(cmd, func(w io.Writer) (int, error) {
				return program.Write(w, p, skipInvalid, cmd.ErrOrStderr())
			})
		})
	}
	return cmd
}
