package main

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/ryan/rflib/internal/domain"
	"github.com/ryan/rflib/internal/model"
	"github.com/ryan/rflib/internal/program"
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
model's levels and held to FRS, GMRS, MURS, and CB limits.`}
	cmd.AddCommand(newRadioAddCommand(), newRadioListCommand(), newRadioShowCommand(), newRadioUpdateCommand(), newRadioDeleteCommand(),
		newRadioChannelsCommand(), newRadioSyncCommand(), newRadioExportCommand())
	return cmd
}

func radioFields(cmd *cobra.Command) {
	cmd.Flags().String("model", "", "Radio model (see rflib model list)")
	cmd.Flags().String("default-power", "", "Power for channels without their own or their group's: low, medium, high, or max")
	cmd.Flags().String("dedupe", "", "Tell apart duplicate names: none, index, or freq")
	cmd.Flags().String("sync-tag", "", "radio sync adds channels with this tag")
	cmd.Flags().String("sync-group", "", "radio sync adds channels in this group (ID or name)")
	cmd.Flags().String("sync-service", "", "radio sync adds channels in this service")
	cmd.Flags().String("notes", "", "Notes")
}

func applyRadioFlags(cmd *cobra.Command, s *store.Store, r *domain.Radio, all bool) error {
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
	if set("sync-tag") {
		r.Sync.Tag = strings.ToLower(strings.TrimSpace(getString(cmd, "sync-tag")))
	}
	if set("sync-service") {
		svc, err := domain.ParseService(getString(cmd, "sync-service"))
		if err != nil {
			return err
		}
		r.Sync.Service = svc
	}
	if set("sync-group") {
		r.Sync.GroupID = ""
		if ref := getString(cmd, "sync-group"); ref != "" {
			g, err := s.GetGroup(ctx(cmd), ref)
			if err != nil {
				return err
			}
			r.Sync.GroupID = g.ID
		}
	}
	return nil
}

func newRadioAddCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "add NAME", Short: "Add one of your radios", Args: cobra.ExactArgs(1)}
	radioFields(cmd)
	_ = cmd.MarkFlagRequired("model")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return withStore(cmd, func(s *store.Store) error {
			r := domain.Radio{Name: args[0]}
			if err := applyRadioFlags(cmd, s, &r, true); err != nil {
				return err
			}
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
			sync := "off"
			if !r.Sync.Empty() {
				var parts []string
				if r.Sync.Tag != "" {
					parts = append(parts, "tag "+r.Sync.Tag)
				}
				if r.Sync.GroupID != "" {
					if g, err := s.GetGroup(ctx(cmd), r.Sync.GroupID); err == nil {
						parts = append(parts, "group "+g.Name)
					}
				}
				if r.Sync.Service != "" {
					parts = append(parts, "service "+string(r.Sync.Service))
				}
				sync = strings.Join(parts, ", ")
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s\nname: %s\nmodel: %s\nchannels: %d\ndefault power: %s\ndedupe: %s\nsync: %s\nnotes: %s\n",
				r.ID, r.Name, r.Model, len(es), powerName(r.DefaultPower), orDefault(r.Dedupe, "none"), sync, r.Notes)
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
			if err := applyRadioFlags(cmd, s, &r, false); err != nil {
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
		Long: `Append channels to the end of a radio's list, by --id or by filter (--tag,
--group, --service, --lat/--lon/--radius-km), in --sort order. Channels already
on the radio are skipped, so the existing order never changes.`}
	cmd.Flags().StringSlice("id", nil, "Channel IDs (repeat or comma-separate)")
	cmd.Flags().String("sort", "name", "Order for channels added by filter: name, freq, or group")
	filterFields(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		ids, _ := cmd.Flags().GetStringSlice("id")
		if len(ids) == 0 && !filtered(cmd) {
			return fmt.Errorf("give channels with --id, or a filter such as --tag")
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
	cmd := &cobra.Command{Use: "sync RADIO", Short: "Append new channels matching the radio's saved filter", Args: cobra.ExactArgs(1),
		Long: `Append channels matching the radio's saved filter (radio update --sync-tag,
--sync-group, --sync-service) that aren't on it yet. Nothing is removed or
reordered.`}
	cmd.Flags().String("sort", "name", "Order for the added channels: name, freq, or group")
	cmd.Flags().Bool("dry-run", false, "List what would be added without changing the radio")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		return withStore(cmd, func(s *store.Store) error {
			r, err := s.GetRadio(ctx(cmd), args[0])
			if err != nil {
				return err
			}
			if r.Sync.Empty() {
				return fmt.Errorf("%s has no sync filter; set one with: rflib radio update %s --sync-tag TAG", r.Name, r.Name)
			}
			cs, err := s.ListChannels(ctx(cmd), store.ChannelFilter{Tag: r.Sync.Tag, GroupID: r.Sync.GroupID, Service: r.Sync.Service})
			if err != nil {
				return err
			}
			if err := sortChannels(cs, getString(cmd, "sort")); err != nil {
				return err
			}
			es, err := s.RadioEntries(ctx(cmd), r.ID)
			if err != nil {
				return err
			}
			on := map[string]bool{}
			for _, e := range es {
				on[e.Channel.ID] = true
			}
			var ids []string
			for _, c := range cs {
				if !on[c.ID] {
					ids = append(ids, c.ID)
					if dryRun {
						fmt.Fprintf(cmd.OutOrStdout(), "would add\t%s\t%s\n", c.Name, domain.FormatFrequencyMHz(c.ReceiveHz))
					}
				}
			}
			if dryRun {
				return nil
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
