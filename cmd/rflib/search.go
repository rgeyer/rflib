package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ryan/rflib/internal/app"
	"github.com/ryan/rflib/internal/domain"
	"github.com/ryan/rflib/internal/query"
	"github.com/spf13/cobra"
)

const queryHelp = `Queries use field:value terms. A space means AND, OR binds tighter than
AND, and a leading - (or NOT) negates. Parentheses group, and quotes keep
spaces in a value. Bare words match names, short names, notes, and group
names. @name uses a saved search.

  name:TEXT notes:TEXT      text, ignoring case
  service:SVC|none          tag:TAG          group:NAME|none
  freq: rx: tx:             MHz: 146.52, 144..148, >=420, ..30
  band:2m|70cm|gmrs|noaa|…  duplex:+|-|split|simplex|off
  tone: txtone: rxtone:     131.8, dcs:023, ctcss, dcs, or none (as programmed)
  mode:FM|NFM|AM|USB|LSB    power:low|medium|high|max|none
  squelch:yes|no            skip:yes|no
  is:repeater|simplex|rxonly|transmit|skip
  has:location|service|group|tag|tone|power|notes|shortname|radio
  near:LAT,LON,KM           within KM plus the group's radius; channels
                            without a location never match
  radio:NAME|any|none       tunes:RADIO|MODEL  in its bands and modes
  created: updated:         YYYY-MM-DD, >=, <, ranges
  id:ID

Examples:
  service:none freq:144..148 -tag:hf is:repeater
  tag:ham (near:34.64,-120.46,40 OR -has:location)
  band:2m OR band:70cm

As arguments, a query starting with - needs -- before it so it isn't read as
a flag: rflib channel list -- -tag:hf. Quoting it or using -q also works.`

// filterFields adds --query and its shorthand flags to a command that
// selects channels.
func filterFields(cmd *cobra.Command) {
	cmd.Flags().StringP("query", "q", "", "Search query (see rflib help search)")
	cmd.Flags().String("group", "", "Shorthand for group:NAME")
	cmd.Flags().String("tag", "", "Shorthand for tag:TAG")
	cmd.Flags().String("service", "", "Shorthand for service:SVC")
	cmd.Flags().String("lat", "", "With --lon and --radius-km, shorthand for near:LAT,LON,KM")
	cmd.Flags().String("lon", "", "Longitude")
	cmd.Flags().String("radius-km", "", "Radius in kilometers")
}

func filtered(cmd *cobra.Command) bool {
	for _, name := range []string{"query", "group", "tag", "service", "lat", "lon", "radius-km"} {
		if cmd.Flags().Changed(name) {
			return true
		}
	}
	return false
}

// channelQuery combines --query, extra query words, and the shorthand
// flags into one query.
func channelQuery(cmd *cobra.Command, words []string) (query.Node, error) {
	var nodes []query.Node
	for _, text := range []string{getString(cmd, "query"), strings.Join(words, " ")} {
		if strings.TrimSpace(text) == "" {
			continue
		}
		n, err := query.Parse(text)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, n)
	}
	for _, field := range []string{"group", "tag", "service"} {
		if v := strings.TrimSpace(getString(cmd, field)); v != "" {
			nodes = append(nodes, query.Term{Field: field, Value: v})
		}
	}
	lat, lon, km := getString(cmd, "lat"), getString(cmd, "lon"), getString(cmd, "radius-km")
	if lat != "" || lon != "" || km != "" {
		if lat == "" || lon == "" || km == "" {
			return nil, fmt.Errorf("--lat, --lon, and --radius-km must be supplied together")
		}
		nodes = append(nodes, query.Term{Field: "near", Value: lat + "," + lon + "," + km})
	}
	return query.Join(nodes...), nil
}

// filteredChannels returns the channels a command's filter flags select.
func filteredChannels(cmd *cobra.Command, a *app.Service) ([]domain.Channel, error) {
	n, err := channelQuery(cmd, nil)
	if err != nil {
		return nil, err
	}
	return a.FindChannels(ctx(cmd), n)
}

// --- saved searches ---

func newSearchCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "search", Short: "Save named searches for use as @name",
		Long: "Save named searches. Use one in any query as @name, including in other\nsearches and radio sources.\n\n" + queryHelp}
	cmd.AddCommand(newSearchAddCommand(), newSearchListCommand(), newSearchShowCommand(), newSearchUpdateCommand(), newSearchDeleteCommand())
	return cmd
}

func newSearchAddCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "add NAME QUERY...", Short: "Save a search", Args: cobra.MinimumNArgs(2)}
	cmd.Flags().String("notes", "", "Notes")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		x := domain.SavedSearch{Name: args[0], Query: strings.Join(args[1:], " "), Notes: getString(cmd, "notes")}
		return withApp(cmd, func(a *app.Service) error {
			x, err := a.CreateSearch(ctx(cmd), x)
			if err == nil {
				fmt.Fprintln(cmd.OutOrStdout(), x.ID)
			}
			return err
		})
	}
	return cmd
}

func newSearchListCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "list", Short: "List saved searches and how many channels each matches", Args: cobra.NoArgs}
	formatFlag(cmd)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		format, err := getFormat(cmd)
		if err != nil {
			return err
		}
		return withApp(cmd, func(a *app.Service) error {
			xs, err := a.ListSearches(ctx(cmd))
			if err != nil {
				return err
			}
			if format == "json" {
				type row struct {
					ID      string `json:"id"`
					Name    string `json:"name"`
					Query   string `json:"query"`
					Matches int    `json:"matches"`
					Error   string `json:"error,omitempty"`
					Notes   string `json:"notes"`
				}
				rows := make([]row, len(xs))
				for i, x := range xs {
					rows[i] = row{ID: x.ID, Name: x.Name, Query: x.Query, Matches: x.Matches, Notes: x.Notes}
					if x.Err != nil {
						rows[i].Error = x.Err.Error()
					}
				}
				return writeJSON(cmd.OutOrStdout(), rows)
			}
			for _, x := range xs {
				fmt.Fprintf(cmd.OutOrStdout(), "@%s\t%s\t%s\n", x.Name, matchesText(x), x.Query)
			}
			return nil
		})
	}
	return cmd
}

func matchesText(x app.SearchSummary) string {
	if x.Err != nil {
		return "error: " + x.Err.Error()
	}
	return strconv.Itoa(x.Matches)
}

func newSearchShowCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "show NAME", Short: "Show a saved search and what uses it", Args: cobra.ExactArgs(1)}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return withApp(cmd, func(a *app.Service) error {
			x, err := a.SearchDetail(ctx(cmd), args[0])
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s\nname: @%s\nquery: %s\nmatches: %s\nused by: %s\nnotes: %s\n",
				x.ID, x.Name, x.Query, matchesText(x.SearchSummary), orDefault(strings.Join(x.Users, ", "), "nothing"), x.Notes)
			return nil
		})
	}
	return cmd
}

func newSearchUpdateCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "update NAME", Short: "Change a saved search; only supplied flags change", Args: cobra.ExactArgs(1)}
	cmd.Flags().String("name", "", "New name (not while something uses the search)")
	cmd.Flags().String("query", "", "New query")
	cmd.Flags().String("notes", "", "Notes")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return withApp(cmd, func(a *app.Service) error {
			x, err := a.GetSearch(ctx(cmd), args[0])
			if err != nil {
				return err
			}
			if cmd.Flags().Changed("name") {
				x.Name = getString(cmd, "name")
			}
			if cmd.Flags().Changed("query") {
				x.Query = getString(cmd, "query")
			}
			if cmd.Flags().Changed("notes") {
				x.Notes = getString(cmd, "notes")
			}
			_, err = a.UpdateSearch(ctx(cmd), x)
			return err
		})
	}
	return cmd
}

func newSearchDeleteCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "delete NAME", Short: "Delete a saved search (not while something uses it)", Args: cobra.ExactArgs(1)}
	cmd.Flags().Bool("yes", false, "Confirm deletion")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := requireYes(cmd, "saved search"); err != nil {
			return err
		}
		return withApp(cmd, func(a *app.Service) error { return a.DeleteSearch(ctx(cmd), args[0]) })
	}
	return cmd
}
