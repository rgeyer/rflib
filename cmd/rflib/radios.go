package main

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/ryan/rflib/internal/domain"
	"github.com/ryan/rflib/internal/model"
	"github.com/ryan/rflib/internal/program"
	"github.com/ryan/rflib/internal/query"
	"github.com/ryan/rflib/internal/store"
	"github.com/spf13/cobra"
)

// --- models ---

func newModelCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "model", Short: "List the radio models rflib can program"}
	list := &cobra.Command{Use: "list", Short: "List radio models", Args: cobra.NoArgs}
	list.RunE = func(cmd *cobra.Command, _ []string) error {
		all, err := model.All()
		if err != nil {
			return err
		}
		for _, m := range all {
			fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\n", m.Name, m.Description, formatName(m))
		}
		return nil
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
channel's power, else its group's, else the radio's default, mapped to the
model's levels and held to FRS, GMRS, MURS, and CB limits.

Sources: a radio can keep searches (rflib radio sources) that radio sync uses
to add new matching channels, each in its own part of the list.`}
	cmd.AddCommand(newRadioAddCommand(), newRadioListCommand(), newRadioShowCommand(), newRadioUpdateCommand(), newRadioDeleteCommand(),
		newRadioChannelsCommand(), newRadioSourcesCommand(), newRadioSyncCommand(), newRadioExportCommand())
	return cmd
}

func radioFields(cmd *cobra.Command) {
	cmd.Flags().String("model", "", "Radio model (see rflib model list)")
	cmd.Flags().String("default-power", "", "Power for channels without their own or their group's: low, medium, high, or max")
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
		return withStore(cmd, func(s *store.Store) error {
			r, err := s.CreateRadio(ctx(cmd), r)
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
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		return withStore(cmd, func(s *store.Store) error {
			rs, err := s.ListRadios(ctx(cmd))
			if err != nil {
				return err
			}
			for _, r := range rs {
				es, err := s.RadioEntries(ctx(cmd), r.ID)
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%d channels\n", r.Name, r.Model, len(es))
			}
			return nil
		})
	}
	return cmd
}

func newRadioShowCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "show RADIO", Short: "Show a radio's settings", Args: cobra.ExactArgs(1)}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return withStore(cmd, func(s *store.Store) error {
			r, err := s.GetRadio(ctx(cmd), args[0])
			if err != nil {
				return err
			}
			es, err := s.RadioEntries(ctx(cmd), r.ID)
			if err != nil {
				return err
			}
			sources, err := s.RadioSources(ctx(cmd), r.ID)
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
			writeSources(out, sources, es)
			return nil
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
		return withStore(cmd, func(s *store.Store) error {
			r, err := s.GetRadio(ctx(cmd), args[0])
			if err != nil {
				return err
			}
			if cmd.Flags().Changed("name") {
				r.Name = getString(cmd, "name")
			}
			if err := applyRadioFlags(cmd, &r, false); err != nil {
				return err
			}
			_, err = s.UpdateRadio(ctx(cmd), r)
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
		return withStore(cmd, func(s *store.Store) error { return s.DeleteRadio(ctx(cmd), args[0]) })
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

// loadPlan loads a radio and plans its memories.
func loadPlan(cmd *cobra.Command, s *store.Store, ref string) (program.Plan, []domain.RadioEntry, error) {
	r, err := s.GetRadio(ctx(cmd), ref)
	if err != nil {
		return program.Plan{}, nil, err
	}
	m, err := model.Find(r.Model)
	if err != nil {
		return program.Plan{}, nil, err
	}
	es, err := s.RadioEntries(ctx(cmd), r.ID)
	if err != nil {
		return program.Plan{}, nil, err
	}
	p, err := program.Build(r, m, es)
	return p, es, err
}

func printWarnings(w io.Writer, p program.Plan) {
	for _, msg := range p.Warnings {
		fmt.Fprintln(w, "warning:", msg)
	}
}

func newRadioChannelsListCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "list RADIO", Short: "Show each memory as it will be programmed", Args: cobra.ExactArgs(1),
		Long: `Show each memory as it will be programmed: number, name on the radio and how
it was fitted, power level, scan, frequency, and the library channel. "§" marks
a section start; "*" marks a value the radio overrides.`}
	formatFlag(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		format, err := getFormat(cmd)
		if err != nil {
			return err
		}
		return withStore(cmd, func(s *store.Store) error {
			p, es, err := loadPlan(cmd, s, args[0])
			if err != nil {
				return err
			}
			if format == "json" {
				type row struct {
					Memory       int    `json:"memory"`
					SectionStart bool   `json:"section_start"`
					RadioName    string `json:"radio_name"`
					NameMethod   string `json:"name_method"`
					Power        string `json:"power"`
					Scan         bool   `json:"scan"`
					ReceiveMHz   string `json:"receive_mhz"`
					ChannelID    string `json:"channel_id"`
					ChannelName  string `json:"channel_name"`
				}
				rows := make([]row, len(p.Memories))
				for i, mem := range p.Memories {
					rows[i] = row{mem.Location, es[i].SectionStart != nil, mem.Channel.Name, string(mem.Name.Method), powerLabel(mem),
						!mem.Channel.Skip, domain.FormatFrequencyMHz(mem.Channel.ReceiveHz), mem.Original.ID, mem.Original.Name}
				}
				return writeJSON(cmd.OutOrStdout(), rows)
			}
			out := cmd.OutOrStdout()
			for i, mem := range p.Memories {
				e := es[i]
				fmt.Fprintf(out, "%s%d\t%s%s\t%s\t%s%s\t%s%s\t%s\t%s\t%s\n",
					mark(e.SectionStart != nil, "§"), mem.Location,
					mem.Channel.Name, mark(e.Name != "", "*"), mem.Name.Method,
					powerLabel(mem), mark(e.Power != "", "*"),
					scanText(mem.Channel.Skip), mark(e.Skip != nil, "*"),
					domain.FormatFrequencyMHz(mem.Channel.ReceiveHz), mem.Original.Name, mem.Original.ID)
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

func newRadioChannelsAddCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "add RADIO", Short: "Append channels to the end of a radio's list", Args: cobra.ExactArgs(1),
		Long: `Append channels to the end of a radio's list, by --id or by --query (or the
shorthand --tag, --group, --service, --lat/--lon/--radius-km), in --sort
order. Channels already on the radio are skipped, so the existing order never
changes. To keep adding a query's new matches, make it a source instead:
rflib radio sources add.`}
	cmd.Flags().StringSlice("id", nil, "Channel IDs (repeat or comma-separate)")
	cmd.Flags().String("sort", "name", "Order for channels added by filter: name, freq, or group")
	filterFields(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		ids, _ := cmd.Flags().GetStringSlice("id")
		if len(ids) == 0 && !filtered(cmd) {
			return fmt.Errorf("give channels with --id or --query")
		}
		return withStore(cmd, func(s *store.Store) error {
			r, err := s.GetRadio(ctx(cmd), args[0])
			if err != nil {
				return err
			}
			for _, id := range ids {
				if _, err := s.GetChannel(ctx(cmd), id); err != nil {
					return err
				}
			}
			if filtered(cmd) {
				cs, err := filteredChannels(cmd, s)
				if err != nil {
					return err
				}
				if err := sortChannels(cs, getString(cmd, "sort")); err != nil {
					return err
				}
				for _, c := range cs {
					ids = append(ids, c.ID)
				}
			}
			n, err := s.AddRadioChannels(ctx(cmd), r.ID, ids)
			if err == nil {
				fmt.Fprintf(cmd.OutOrStdout(), "Added %d channels to %s\n", n, r.Name)
			}
			return err
		})
	}
	return cmd
}

func newRadioChannelsRemoveCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "remove RADIO", Short: "Take channels off a radio (they stay in the library)", Args: cobra.ExactArgs(1)}
	cmd.Flags().StringSlice("id", nil, "Channel IDs (repeat or comma-separate)")
	_ = cmd.MarkFlagRequired("id")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		ids, _ := cmd.Flags().GetStringSlice("id")
		return withStore(cmd, func(s *store.Store) error {
			r, err := s.GetRadio(ctx(cmd), args[0])
			if err != nil {
				return err
			}
			n, err := s.RemoveRadioChannels(ctx(cmd), r.ID, ids)
			if err == nil {
				fmt.Fprintf(cmd.OutOrStdout(), "Removed %d channels from %s\n", n, r.Name)
			}
			return err
		})
	}
	return cmd
}

func newRadioChannelsMoveCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "move RADIO", Short: "Move a channel within a radio's list", Args: cobra.ExactArgs(1)}
	cmd.Flags().String("id", "", "Channel ID to move (required)")
	cmd.Flags().String("before", "", "Move just before this channel ID")
	cmd.Flags().String("after", "", "Move just after this channel ID")
	cmd.Flags().Int("to", 0, "Move to this position in the list, from 1")
	cmd.Flags().Bool("top", false, "Move to the top")
	cmd.Flags().Bool("bottom", false, "Move to the bottom")
	cmd.MarkFlagsMutuallyExclusive("before", "after", "to", "top", "bottom")
	cmd.MarkFlagsOneRequired("before", "after", "to", "top", "bottom")
	_ = cmd.MarkFlagRequired("id")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		id := getString(cmd, "id")
		return withStore(cmd, func(s *store.Store) error {
			r, err := s.GetRadio(ctx(cmd), args[0])
			if err != nil {
				return err
			}
			es, err := s.RadioEntries(ctx(cmd), r.ID)
			if err != nil {
				return err
			}
			index := func(ref string) (int, error) {
				for _, e := range es {
					if e.Channel.ID == ref {
						return e.Position, nil
					}
				}
				return 0, fmt.Errorf("channel %q is not on %s: %w", ref, r.Name, store.ErrNotFound)
			}
			from, err := index(id)
			if err != nil {
				return err
			}
			var to int
			switch {
			case cmd.Flags().Changed("top"):
				to = 0
			case cmd.Flags().Changed("bottom"):
				to = len(es)
			case cmd.Flags().Changed("to"):
				n, _ := cmd.Flags().GetInt("to")
				if n < 1 {
					return fmt.Errorf("--to counts from 1")
				}
				to = n - 1
			default:
				ref := getString(cmd, "before") + getString(cmd, "after")
				if to, err = index(ref); err != nil {
					return err
				}
				// Positions are counted after the moving channel is taken out.
				if to > from {
					to--
				}
				if cmd.Flags().Changed("after") {
					to++
				}
			}
			return s.MoveRadioChannel(ctx(cmd), r.ID, id, to)
		})
	}
	return cmd
}

func newRadioChannelsSetCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "set RADIO", Short: "Set a radio's overrides for one of its channels", Args: cobra.ExactArgs(1),
		Long: `Set a radio's overrides for one of its channels. Only supplied flags change;
an empty value removes that override, so the channel's own setting applies.`}
	cmd.Flags().String("id", "", "Channel ID (required)")
	cmd.Flags().String("name", "", "Name on this radio")
	cmd.Flags().String("power", "", "Power on this radio: low, medium, high, or max")
	cmd.Flags().String("scan", "", "Scan on this radio: yes, no, or default (follow the channel)")
	cmd.Flags().String("section-start", "", "Start a section at this memory number (empty ends it)")
	_ = cmd.MarkFlagRequired("id")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		id := getString(cmd, "id")
		return withStore(cmd, func(s *store.Store) error {
			r, err := s.GetRadio(ctx(cmd), args[0])
			if err != nil {
				return err
			}
			es, err := s.RadioEntries(ctx(cmd), r.ID)
			if err != nil {
				return err
			}
			var e *domain.RadioEntry
			for i := range es {
				if es[i].Channel.ID == id {
					e = &es[i]
				}
			}
			if e == nil {
				return fmt.Errorf("channel %q is not on %s: %w", id, r.Name, store.ErrNotFound)
			}
			if cmd.Flags().Changed("name") {
				e.Name = getString(cmd, "name")
			}
			if cmd.Flags().Changed("power") {
				if e.Power, err = domain.ParsePowerIntent(getString(cmd, "power")); err != nil {
					return err
				}
			}
			if cmd.Flags().Changed("scan") {
				switch v := strings.ToLower(getString(cmd, "scan")); v {
				case "yes", "no":
					skip := v == "no"
					e.Skip = &skip
				case "default", "":
					e.Skip = nil
				default:
					return fmt.Errorf("--scan must be yes, no, or default")
				}
			}
			if cmd.Flags().Changed("section-start") {
				e.SectionStart = nil
				if v := strings.TrimSpace(getString(cmd, "section-start")); v != "" {
					n, err := strconv.Atoi(v)
					if err != nil {
						return fmt.Errorf("--section-start must be a memory number")
					}
					e.SectionStart = &n
				}
			}
			if err := s.SetRadioEntry(ctx(cmd), r.ID, *e); err != nil {
				return err
			}
			// Check the result still plans, so a bad section start is caught now.
			if _, _, err := loadPlan(cmd, s, r.ID); err != nil {
				return fmt.Errorf("saved, but %s no longer exports: %w", r.Name, err)
			}
			return nil
		})
	}
	return cmd
}

func newRadioSyncCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "sync RADIO", Short: "Add new channels matching the radio's sources", Args: cobra.ExactArgs(1),
		Long: `Add channels matching each of the radio's sources that aren't on it yet.
Each source's new channels go after the last channel it added, in the
source's sort order; an earlier source claims a channel first. Nothing is
removed or reordered.`}
	cmd.Flags().Bool("dry-run", false, "List what would be added without changing the radio")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		return withStore(cmd, func(s *store.Store) error {
			r, err := s.GetRadio(ctx(cmd), args[0])
			if err != nil {
				return err
			}
			sources, err := s.RadioSources(ctx(cmd), r.ID)
			if err != nil {
				return err
			}
			if len(sources) == 0 {
				return fmt.Errorf("%s has no sources; add one with: rflib radio sources add %s QUERY", r.Name, r.Name)
			}
			es, err := s.RadioEntries(ctx(cmd), r.ID)
			if err != nil {
				return err
			}
			claimed := map[string]bool{}
			for _, e := range es {
				claimed[e.Channel.ID] = true
			}
			out := cmd.OutOrStdout()
			total := 0
			for _, src := range sources {
				n, err := query.Parse(src.Query)
				if err != nil {
					return fmt.Errorf("source %d: %w", src.Position+1, err)
				}
				cs, err := findChannels(cmd, s, n)
				if err != nil {
					return fmt.Errorf("source %d: %w", src.Position+1, err)
				}
				if err := sortChannels(cs, src.Sort); err != nil {
					return err
				}
				var ids []string
				for _, c := range cs {
					if !claimed[c.ID] {
						claimed[c.ID] = true
						ids = append(ids, c.ID)
						if dryRun {
							fmt.Fprintf(out, "would add\tsource %d\t%s\t%s\n", src.Position+1, c.Name, domain.FormatFrequencyMHz(c.ReceiveHz))
						}
					}
				}
				if dryRun || len(ids) == 0 {
					continue
				}
				added, err := s.AddSourceChannels(ctx(cmd), src, ids)
				if err != nil {
					return err
				}
				fmt.Fprintf(out, "Source %d (%s): added %d\n", src.Position+1, src.Query, added)
				total += added
			}
			if dryRun {
				return nil
			}
			fmt.Fprintf(out, "Added %d channels to %s\n", total, r.Name)
			if _, _, err := loadPlan(cmd, s, r.ID); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s no longer exports: %v\n", r.Name, err)
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

func writeSources(w io.Writer, sources []domain.RadioSource, es []domain.RadioEntry) {
	count := map[string]int{}
	for _, e := range es {
		count[e.SourceID]++
	}
	for _, src := range sources {
		section := ""
		if src.SectionStart != nil {
			section = fmt.Sprintf("§%d", *src.SectionStart)
		}
		fmt.Fprintf(w, "%d\t%s\tsort %s\t%d channels\t%s\n", src.Position+1, orDefault(section, "-"), src.Sort, count[src.ID], src.Query)
	}
}

// radioSource finds a radio and one of its sources by number (from 1).
func radioSource(cmd *cobra.Command, s *store.Store, radioRef, number string) (domain.Radio, domain.RadioSource, error) {
	r, err := s.GetRadio(ctx(cmd), radioRef)
	if err != nil {
		return r, domain.RadioSource{}, err
	}
	sources, err := s.RadioSources(ctx(cmd), r.ID)
	if err != nil {
		return r, domain.RadioSource{}, err
	}
	n, err := strconv.Atoi(number)
	if err != nil || n < 1 || n > len(sources) {
		return r, domain.RadioSource{}, fmt.Errorf("%s has sources 1-%d; see rflib radio sources list %s", r.Name, len(sources), r.Name)
	}
	return r, sources[n-1], nil
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
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return withStore(cmd, func(s *store.Store) error {
			r, err := s.GetRadio(ctx(cmd), args[0])
			if err != nil {
				return err
			}
			sources, err := s.RadioSources(ctx(cmd), r.ID)
			if err != nil {
				return err
			}
			es, err := s.RadioEntries(ctx(cmd), r.ID)
			if err != nil {
				return err
			}
			writeSources(cmd.OutOrStdout(), sources, es)
			return nil
		})
	}
	return cmd
}

func newRadioSourcesAddCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "add RADIO QUERY...", Short: "Add a source to the end of a radio's sources", Args: cobra.MinimumNArgs(2)}
	cmd.Flags().String("sort", "name", "Order its channels are added in: name, freq, or group")
	cmd.Flags().String("section-start", "", "Start the source's channels at this memory number")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		section, err := parseSectionStart(getString(cmd, "section-start"))
		if err != nil {
			return err
		}
		return withStore(cmd, func(s *store.Store) error {
			r, err := s.GetRadio(ctx(cmd), args[0])
			if err != nil {
				return err
			}
			src := domain.RadioSource{RadioID: r.ID, Query: strings.Join(args[1:], " "), Sort: getString(cmd, "sort"), SectionStart: section}
			if _, err := checkQuery(cmd, s, src.Query, nil); err != nil {
				return err
			}
			src, err = s.AddRadioSource(ctx(cmd), src)
			if err == nil {
				fmt.Fprintf(cmd.OutOrStdout(), "Added source %d to %s; run rflib radio sync %s to add its channels\n", src.Position+1, r.Name, r.Name)
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
	cmd.Flags().String("sort", "", "Order its channels are added in: name, freq, or group")
	cmd.Flags().String("section-start", "", "Start the source's channels at this memory number (empty ends it)")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return withStore(cmd, func(s *store.Store) error {
			r, src, err := radioSource(cmd, s, args[0], args[1])
			if err != nil {
				return err
			}
			if cmd.Flags().Changed("query") {
				src.Query = getString(cmd, "query")
				if _, err := checkQuery(cmd, s, src.Query, nil); err != nil {
					return err
				}
			}
			if cmd.Flags().Changed("sort") {
				src.Sort = getString(cmd, "sort")
			}
			if cmd.Flags().Changed("section-start") {
				if src.SectionStart, err = parseSectionStart(getString(cmd, "section-start")); err != nil {
					return err
				}
			}
			if err := s.UpdateRadioSource(ctx(cmd), src); err != nil {
				return err
			}
			if _, _, err := loadPlan(cmd, s, r.ID); err != nil {
				return fmt.Errorf("saved, but %s no longer exports: %w", r.Name, err)
			}
			return nil
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
		return withStore(cmd, func(s *store.Store) error {
			_, src, err := radioSource(cmd, s, args[0], args[1])
			if err != nil {
				return err
			}
			return s.MoveRadioSource(ctx(cmd), src, to-1)
		})
	}
	return cmd
}

func newRadioSourcesRemoveCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "remove RADIO N", Short: "Remove a source; its channels stay unless --with-channels", Args: cobra.ExactArgs(2)}
	cmd.Flags().Bool("with-channels", false, "Also take the channels it added off the radio")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		withChannels, _ := cmd.Flags().GetBool("with-channels")
		return withStore(cmd, func(s *store.Store) error {
			r, src, err := radioSource(cmd, s, args[0], args[1])
			if err != nil {
				return err
			}
			n, err := s.RemoveRadioSource(ctx(cmd), src, withChannels)
			if err == nil {
				fmt.Fprintf(cmd.OutOrStdout(), "Removed source %s from %s", args[1], r.Name)
				if withChannels {
					fmt.Fprintf(cmd.OutOrStdout(), " and %d channels", n)
				}
				fmt.Fprintln(cmd.OutOrStdout())
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
		return withStore(cmd, func(s *store.Store) error {
			p, _, err := loadPlan(cmd, s, args[0])
			if err != nil {
				return err
			}
			printWarnings(cmd.ErrOrStderr(), p)
			return writeOutput(cmd, len(p.Memories), func(w io.Writer) error {
				_, err := program.Write(w, p, skipInvalid, cmd.ErrOrStderr())
				return err
			})
		})
	}
	return cmd
}
