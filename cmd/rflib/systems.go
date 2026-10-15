package main

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/ryan/rflib/internal/app"
	"github.com/ryan/rflib/internal/domain"
	"github.com/spf13/cobra"
)

// Organizations, systems, and plans live in the shared list, so these
// commands work the same from any library. Changes are maintainer
// operations, recorded with --reason.

func recordReasonFlag(cmd *cobra.Command) {
	cmd.Flags().String("reason", "", "Why, recorded in the history (required)")
	_ = cmd.MarkFlagRequired("reason")
}

// orgFlags adds --operators and --partners to a command that attaches
// organizations.
func orgFlags(cmd *cobra.Command) {
	cmd.Flags().String("operators", "", "Comma-separated organizations that run it, by name (empty clears on update)")
	cmd.Flags().String("partners", "", "Comma-separated organizations that sponsor or support it (empty clears on update)")
}

// applyOrgFlags replaces the roles --operators and --partners name, and
// keeps the others.
func applyOrgFlags(cmd *cobra.Command, a *app.Service, rs []domain.OrgRef) ([]domain.OrgRef, error) {
	for _, f := range []struct {
		flag string
		role domain.OrgRole
	}{{"operators", domain.RoleOperator}, {"partners", domain.RolePartner}} {
		if !cmd.Flags().Changed(f.flag) {
			continue
		}
		var keep []domain.OrgRef
		for _, r := range rs {
			if r.Role != f.role {
				keep = append(keep, r)
			}
		}
		for _, name := range splitTags(getString(cmd, f.flag)) {
			r, err := a.OrgRef(ctx(cmd), strings.TrimSpace(name), f.role)
			if err != nil {
				return nil, fmt.Errorf("--%s: %w", f.flag, err)
			}
			keep = append(keep, r)
		}
		rs = keep
	}
	return rs, nil
}

// printHistory writes revisions, oldest first, each with what changed
// from the one before.
func printHistory(out io.Writer, revs []domain.RecordRevision) {
	var prev []byte
	for _, r := range revs {
		fmt.Fprintf(out, "revision %d\t%s\t%s\n", r.Number, r.At.Local().Format("2006-01-02 15:04"), orDefault(r.Reason, "(no reason given)"))
		if prev != nil {
			for _, line := range domain.RecordChanges(prev, r.Record) {
				fmt.Fprintf(out, "\t%s\n", line)
			}
		}
		prev = r.Record
	}
}

func newHistoryCommand(kind, what string) *cobra.Command {
	return &cobra.Command{Use: "history NAME", Short: "Show the " + what + "'s revisions and what changed in each", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withApp(cmd, func(a *app.Service) error {
				revs, err := a.RecordHistory(ctx(cmd), kind, args[0])
				if err == nil {
					printHistory(cmd.OutOrStdout(), revs)
				}
				return err
			})
		}}
}

// --- organizations ---

const orgHelp = `Organizations are who runs or supports repeaters and systems: clubs,
emergency services, agencies, and individuals. They're plain records in the
shared list, attached to shared channels (rflib shared update --operators,
--partners) and systems, and found with org:NAME.`

func newOrgCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "org", Short: "Record who operates repeaters and systems", Long: orgHelp}
	cmd.AddCommand(newOrgAddCommand(), newOrgListCommand(), newOrgShowCommand(), newOrgUpdateCommand(), newOrgDeleteCommand(),
		newHistoryCommand(app.KindOrganization, "organization"))
	return cmd
}

func orgFieldFlags(cmd *cobra.Command) {
	cmd.Flags().String("kind", "", "club, emergency, agency, or individual (default club)")
	cmd.Flags().String("callsign", "", "Club or station callsign")
	cmd.Flags().String("website", "", "Website, starting https://")
	cmd.Flags().String("notes", "", "Notes")
}

func applyOrgFieldFlags(cmd *cobra.Command, o *domain.Organization) error {
	if cmd.Flags().Changed("kind") {
		k, err := domain.ParseOrgKind(getString(cmd, "kind"))
		if err != nil {
			return err
		}
		o.Kind = k
	}
	for _, f := range []struct {
		name string
		dst  *string
	}{{"callsign", &o.Callsign}, {"website", &o.Website}, {"notes", &o.Notes}} {
		if cmd.Flags().Changed(f.name) {
			*f.dst = getString(cmd, f.name)
		}
	}
	return nil
}

func newOrgAddCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "add NAME", Short: "Add an organization (maintainer)", Args: cobra.ExactArgs(1)}
	orgFieldFlags(cmd)
	recordReasonFlag(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		o := domain.Organization{Name: args[0]}
		if err := applyOrgFieldFlags(cmd, &o); err != nil {
			return err
		}
		return withApp(cmd, func(a *app.Service) error {
			o, err := a.CreateOrg(ctx(cmd), o, getString(cmd, "reason"))
			if err == nil {
				fmt.Fprintln(cmd.OutOrStdout(), o.ID)
			}
			return err
		})
	}
	return cmd
}

var orgListing = listing[domain.Organization]{
	fields: []field[domain.Organization]{
		{name: "name", text: func(o domain.Organization) string { return o.Name }},
		{name: "kind", text: func(o domain.Organization) string { return string(o.Kind) }},
		{name: "callsign", text: func(o domain.Organization) string { return o.Callsign }},
		{name: "website", text: func(o domain.Organization) string { return o.Website }},
		{name: "notes", text: func(o domain.Organization) string { return o.Notes }},
		{name: "id", id: true, text: func(o domain.Organization) string { return o.ID }},
	},
	defaults: []string{"name", "kind", "callsign", "website", "id"},
}

func newOrgListCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "list", Short: "List organizations", Args: cobra.NoArgs}
	orgListing.flags(cmd)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if err := orgListing.check(cmd); err != nil {
			return err
		}
		return withApp(cmd, func(a *app.Service) error {
			orgs, err := a.ListOrgs(ctx(cmd))
			if err != nil {
				return err
			}
			return orgListing.write(cmd, orgs)
		})
	}
	return cmd
}

func newOrgShowCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "show NAME", Short: "Show an organization and the channels and systems it's part of", Args: cobra.ExactArgs(1)}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return withApp(cmd, func(a *app.Service) error {
			d, err := a.OrgDetail(ctx(cmd), args[0])
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "%s\nname: %s\nkind: %s\ncallsign: %s\nwebsite: %s\nnotes: %s\n", d.ID, d.Name, d.Kind, d.Callsign, d.Website, d.Notes)
			fmt.Fprintf(out, "channels: %d\n", len(d.Channels))
			for _, c := range d.Channels {
				fmt.Fprintf(out, "\t%s\t%s\t%s\t%s\n", c.ID, c.Name, domain.FormatFrequencyMHz(c.ReceiveHz), roleIn(c.Orgs, d.ID))
			}
			fmt.Fprintf(out, "systems: %d\n", len(d.Systems))
			for _, x := range d.Systems {
				fmt.Fprintf(out, "\t%s\t%s\t%s\n", x.ID, x.Name, roleIn(x.Orgs, d.ID))
			}
			return nil
		})
	}
	return cmd
}

// roleIn is an organization's roles among rs.
func roleIn(rs []domain.OrgRef, orgID string) string {
	var roles []string
	for _, r := range rs {
		if r.OrgID == orgID {
			roles = append(roles, string(r.Role))
		}
	}
	return strings.Join(roles, ",")
}

func newOrgUpdateCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "update NAME", Short: "Change an organization; only supplied flags change (maintainer)", Args: cobra.ExactArgs(1)}
	cmd.Flags().String("name", "", "New name")
	orgFieldFlags(cmd)
	recordReasonFlag(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return withApp(cmd, func(a *app.Service) error {
			o, err := a.GetOrg(ctx(cmd), args[0])
			if err != nil {
				return err
			}
			if cmd.Flags().Changed("name") {
				o.Name = getString(cmd, "name")
			}
			if err := applyOrgFieldFlags(cmd, &o); err != nil {
				return err
			}
			_, err = a.UpdateOrg(ctx(cmd), o, getString(cmd, "reason"))
			return err
		})
	}
	return cmd
}

func newOrgDeleteCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "delete NAME", Short: "Delete an organization attached to nothing (maintainer)", Args: cobra.ExactArgs(1)}
	cmd.Flags().Bool("yes", false, "Confirm deletion")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := requireYes(cmd, "organization"); err != nil {
			return err
		}
		return withApp(cmd, func(a *app.Service) error { return a.DeleteOrg(ctx(cmd), args[0]) })
	}
	return cmd
}

// --- systems ---

const systemHelp = `A system is a set of interconnected channels, such as a club's linked
repeaters, as its operators intend it: which members are usually linked
(full-time) and which may be (on demand). A system can have nodes of its own
on linking networks, such as a hub's AllStar node, and its members can belong
to different organizations. Members are shared channels; in a library, a
link's ID stands for its shared channel. Find a system's channels with
system:NAME.`

func newSystemCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "system", Short: "Record linked repeater systems", Long: systemHelp}
	members := &cobra.Command{Use: "members", Short: "Add channels to a system or take them out"}
	members.AddCommand(newSystemMembersAddCommand(), newSystemMembersRemoveCommand())
	cmd.AddCommand(newSystemAddCommand(), newSystemListCommand(), newSystemShowCommand(), newSystemUpdateCommand(), newSystemDeleteCommand(),
		members, newHistoryCommand(app.KindSystem, "system"))
	return cmd
}

func systemFieldFlags(cmd *cobra.Command) {
	cmd.Flags().String("link", "", "How the members are linked: allstar, echolink, irlp, wires-x, rf, or other")
	cmd.Flags().String("networks", "", "Comma-separated nodes of the system's own, such as allstar:43763 (empty clears on update)")
	cmd.Flags().String("notes", "", "Notes")
	orgFlags(cmd)
}

func applySystemFlags(cmd *cobra.Command, a *app.Service, x *domain.System) error {
	if cmd.Flags().Changed("link") {
		t, err := domain.ParseLinkType(getString(cmd, "link"))
		if err != nil {
			return err
		}
		x.Link = t
	}
	if cmd.Flags().Changed("networks") {
		ns, err := domain.ParseNetworkIDs(splitTags(getString(cmd, "networks")))
		if err != nil {
			return fmt.Errorf("--networks: %w", err)
		}
		x.Networks = ns
	}
	if cmd.Flags().Changed("notes") {
		x.Notes = getString(cmd, "notes")
	}
	var err error
	x.Orgs, err = applyOrgFlags(cmd, a, x.Orgs)
	return err
}

func newSystemAddCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "add NAME", Short: "Add a system (maintainer)", Args: cobra.ExactArgs(1)}
	systemFieldFlags(cmd)
	recordReasonFlag(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return withApp(cmd, func(a *app.Service) error {
			x := domain.System{Name: args[0]}
			if err := applySystemFlags(cmd, a, &x); err != nil {
				return err
			}
			x, err := a.CreateSystem(ctx(cmd), x, getString(cmd, "reason"))
			if err == nil {
				fmt.Fprintln(cmd.OutOrStdout(), x.ID)
			}
			return err
		})
	}
	return cmd
}

var systemListing = listing[domain.System]{
	fields: []field[domain.System]{
		{name: "name", text: func(x domain.System) string { return x.Name }},
		{name: "link", text: func(x domain.System) string { return string(x.Link) }},
		{name: "networks", text: func(x domain.System) string { return strings.Join(networkStrings(x.Networks), ",") }, json: func(x domain.System) any { return nonNil(networkStrings(x.Networks)) }},
		{name: "members", text: func(x domain.System) string { return strconv.Itoa(len(x.Members)) }, json: func(x domain.System) any { return len(x.Members) }},
		{name: "operators", text: func(x domain.System) string { return strings.Join(orgNames(x.Orgs, domain.RoleOperator), ",") }, json: func(x domain.System) any { return nonNil(orgNames(x.Orgs, domain.RoleOperator)) }},
		{name: "notes", text: func(x domain.System) string { return x.Notes }},
		{name: "id", id: true, text: func(x domain.System) string { return x.ID }},
	},
	defaults: []string{"name", "link", "networks", "members", "operators", "id"},
}

func newSystemListCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "list", Short: "List systems", Args: cobra.NoArgs}
	systemListing.flags(cmd)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if err := systemListing.check(cmd); err != nil {
			return err
		}
		return withApp(cmd, func(a *app.Service) error {
			xs, err := a.ListSystems(ctx(cmd))
			if err != nil {
				return err
			}
			return systemListing.write(cmd, xs)
		})
	}
	return cmd
}

func newSystemShowCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "show NAME", Short: "Show a system and its members", Args: cobra.ExactArgs(1)}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return withApp(cmd, func(a *app.Service) error {
			d, err := a.SystemDetail(ctx(cmd), args[0])
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "%s\nname: %s\nlink: %s\nnetworks: %s\norganizations: %s\nnotes: %s\nmembers: %d\n",
				d.ID, d.Name, d.Link, strings.Join(networkStrings(d.Networks), ", "), orgRefsText(d.Orgs), d.Notes, len(d.Members))
			for i, m := range d.Members {
				c := d.Channels[i]
				fmt.Fprintf(out, "\t%s\t%s\t%s\t%s\t%s\n", c.ID, c.Name, domain.FormatFrequencyMHz(c.ReceiveHz), m.Linking, m.Notes)
			}
			return nil
		})
	}
	return cmd
}

func newSystemUpdateCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "update NAME", Short: "Change a system; only supplied flags change (maintainer)", Args: cobra.ExactArgs(1)}
	cmd.Flags().String("name", "", "New name")
	systemFieldFlags(cmd)
	recordReasonFlag(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return withApp(cmd, func(a *app.Service) error {
			x, err := a.GetSystem(ctx(cmd), args[0])
			if err != nil {
				return err
			}
			if cmd.Flags().Changed("name") {
				x.Name = getString(cmd, "name")
			}
			if err := applySystemFlags(cmd, a, &x); err != nil {
				return err
			}
			_, err = a.UpdateSystem(ctx(cmd), x, getString(cmd, "reason"))
			return err
		})
	}
	return cmd
}

func newSystemDeleteCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "delete NAME", Short: "Delete a system; its channels stay (maintainer)", Args: cobra.ExactArgs(1)}
	cmd.Flags().Bool("yes", false, "Confirm deletion")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := requireYes(cmd, "system"); err != nil {
			return err
		}
		return withApp(cmd, func(a *app.Service) error { return a.DeleteSystem(ctx(cmd), args[0]) })
	}
	return cmd
}

func newSystemMembersAddCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "add SYSTEM", Short: "Add shared channels to a system, or change how they're linked (maintainer)", Args: cobra.ExactArgs(1),
		Long: "Add shared channels to a system, or change how members already in it are\nlinked.\n\n" + stdinIDHelp}
	idsFlag(cmd, "Shared channel IDs, or links' IDs (required)")
	_ = cmd.MarkFlagRequired("id")
	cmd.Flags().Bool("on-demand", false, "Linked on demand rather than full time")
	cmd.Flags().String("notes", "", "How it's linked, such as \"microwave to the hub\"")
	recordReasonFlag(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		refs, err := getIDs(cmd)
		if err != nil {
			return err
		}
		linking := domain.LinkFullTime
		if on, _ := cmd.Flags().GetBool("on-demand"); on {
			linking = domain.LinkOnDemand
		}
		return withApp(cmd, func(a *app.Service) error {
			x, err := a.GetSystem(ctx(cmd), args[0])
			if err != nil {
				return err
			}
			ids, err := a.SharedChannelIDs(ctx(cmd), refs)
			if err != nil {
				return err
			}
			for _, id := range ids {
				m := domain.SystemMember{ChannelID: id, Linking: linking, Notes: getString(cmd, "notes")}
				replaced := false
				for i := range x.Members {
					if x.Members[i].ChannelID == id {
						x.Members[i], replaced = m, true
					}
				}
				if !replaced {
					x.Members = append(x.Members, m)
				}
			}
			x, err = a.UpdateSystem(ctx(cmd), x, getString(cmd, "reason"))
			if err == nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s has %d members\n", x.Name, len(x.Members))
			}
			return err
		})
	}
	return cmd
}

func newSystemMembersRemoveCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "remove SYSTEM", Short: "Take channels out of a system (maintainer)", Args: cobra.ExactArgs(1)}
	idsFlag(cmd, "Shared channel IDs, or links' IDs (required)")
	_ = cmd.MarkFlagRequired("id")
	recordReasonFlag(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		refs, err := getIDs(cmd)
		if err != nil {
			return err
		}
		return withApp(cmd, func(a *app.Service) error {
			x, err := a.GetSystem(ctx(cmd), args[0])
			if err != nil {
				return err
			}
			ids, err := a.SharedChannelIDs(ctx(cmd), refs)
			if err != nil {
				return err
			}
			drop := map[string]bool{}
			for _, id := range ids {
				drop[id] = true
			}
			var keep []domain.SystemMember
			for _, m := range x.Members {
				if !drop[m.ChannelID] {
					keep = append(keep, m)
				}
			}
			x.Members = keep
			x, err = a.UpdateSystem(ctx(cmd), x, getString(cmd, "reason"))
			if err == nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s has %d members\n", x.Name, len(x.Members))
			}
			return err
		})
	}
	return cmd
}

// --- plans ---

const planHelp = `A plan is a communications plan in the shared list: an ordered selection of
shared channels, each with its name and assignment in the plan, laid out as
ICS 205 lists them. An event is a plan with dates. A plan's entries are
shared channels, or query sections that bring in every shared channel they
match (such as system:"SBARC Hub"); section starts set the plan channel
number (ICS 205's Ch #) an entry starts at.

Program a plan onto a radio with rflib radio sources add RADIO --plan NAME
--section-start N: radio sync follows the plan's changes. Plans are
draft, published, or archived, and are never deleted.`

func newPlanCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "plan", Short: "Keep communications plans, such as an event's channels", Long: planHelp}
	entries := &cobra.Command{Use: "entries", Short: "Change a plan's entries", Long: "Change a plan's entries. Entries are numbered from 1 in the order plan show\nlists them."}
	entries.AddCommand(newPlanEntriesAddCommand(), newPlanEntriesRemoveCommand(), newPlanEntriesMoveCommand(), newPlanEntriesSetCommand())
	cmd.AddCommand(newPlanAddCommand(), newPlanListCommand(), newPlanShowCommand(), newPlanChannelsCommand(), newPlanUpdateCommand(), newPlanArchiveCommand(),
		entries, newHistoryCommand(app.KindPlan, "plan"))
	return cmd
}

func planFieldFlags(cmd *cobra.Command) {
	cmd.Flags().String("description", "", "What the plan is for")
	cmd.Flags().String("starts", "", "First day, YYYY-MM-DD (empty clears on update)")
	cmd.Flags().String("ends", "", "Last day, YYYY-MM-DD (empty clears on update)")
	cmd.Flags().String("status", "", "draft, published, or archived (default draft)")
}

func parseDay(flag, v string) (*time.Time, error) {
	if strings.TrimSpace(v) == "" {
		return nil, nil
	}
	t, err := time.Parse(time.DateOnly, strings.TrimSpace(v))
	if err != nil {
		return nil, fmt.Errorf("--%s must be a date, YYYY-MM-DD", flag)
	}
	return &t, nil
}

func applyPlanFlags(cmd *cobra.Command, p *domain.Plan) error {
	if cmd.Flags().Changed("description") {
		p.Description = getString(cmd, "description")
	}
	for _, f := range []struct {
		name string
		dst  **time.Time
	}{{"starts", &p.Starts}, {"ends", &p.Ends}} {
		if cmd.Flags().Changed(f.name) {
			t, err := parseDay(f.name, getString(cmd, f.name))
			if err != nil {
				return err
			}
			*f.dst = t
		}
	}
	if cmd.Flags().Changed("status") {
		st, err := domain.ParsePlanStatus(getString(cmd, "status"))
		if err != nil {
			return err
		}
		p.Status = st
	}
	return nil
}

func newPlanAddCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "add NAME", Short: "Start a plan (maintainer)", Args: cobra.ExactArgs(1)}
	planFieldFlags(cmd)
	recordReasonFlag(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		p := domain.Plan{Name: args[0]}
		if err := applyPlanFlags(cmd, &p); err != nil {
			return err
		}
		return withApp(cmd, func(a *app.Service) error {
			p, err := a.CreatePlan(ctx(cmd), p, getString(cmd, "reason"))
			if err == nil {
				fmt.Fprintln(cmd.OutOrStdout(), p.ID)
			}
			return err
		})
	}
	return cmd
}

func day(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format(time.DateOnly)
}

var planListing = listing[domain.Plan]{
	fields: []field[domain.Plan]{
		{name: "name", text: func(p domain.Plan) string { return p.Name }},
		{name: "status", text: func(p domain.Plan) string { return string(p.Status) }},
		{name: "starts", text: func(p domain.Plan) string { return day(p.Starts) }},
		{name: "ends", text: func(p domain.Plan) string { return day(p.Ends) }},
		{name: "entries", text: func(p domain.Plan) string { return strconv.Itoa(len(p.Entries)) }, json: func(p domain.Plan) any { return len(p.Entries) }},
		{name: "description", text: func(p domain.Plan) string { return p.Description }},
		{name: "id", id: true, text: func(p domain.Plan) string { return p.ID }},
	},
	defaults: []string{"name", "status", "starts", "ends", "entries", "id"},
}

func newPlanListCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "list", Short: "List plans", Args: cobra.NoArgs}
	planListing.flags(cmd)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if err := planListing.check(cmd); err != nil {
			return err
		}
		return withApp(cmd, func(a *app.Service) error {
			ps, err := a.ListPlans(ctx(cmd))
			if err != nil {
				return err
			}
			return planListing.write(cmd, ps)
		})
	}
	return cmd
}

func newPlanShowCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "show NAME", Short: "Show a plan and its entries", Args: cobra.ExactArgs(1)}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return withApp(cmd, func(a *app.Service) error {
			p, err := a.GetPlan(ctx(cmd), args[0])
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "%s\nname: %s\nstatus: %s\nstarts: %s\nends: %s\ndescription: %s\nentries: %d\n",
				p.ID, p.Name, p.Status, day(p.Starts), day(p.Ends), p.Description, len(p.Entries))
			for i, e := range p.Entries {
				what := e.ChannelID
				if what == "" {
					what = "query " + e.Query + " (by " + e.Sort + ")"
				}
				section := ""
				if e.SectionStart != nil {
					section = "§" + strconv.Itoa(*e.SectionStart)
				}
				fmt.Fprintf(out, "\t%d\t%s\t%s\t%s\t%s\t%s\n", i+1, section, what, e.Name, e.Assignment, e.Notes)
			}
			return nil
		})
	}
	return cmd
}

var planRowListing = listing[app.PlanRow]{
	fields: []field[app.PlanRow]{
		{name: "ch", text: func(r app.PlanRow) string { return strconv.Itoa(r.Number) }, json: func(r app.PlanRow) any { return r.Number }},
		{name: "name", text: func(r app.PlanRow) string { return r.Name }},
		{name: "assignment", text: func(r app.PlanRow) string { return r.Assignment }},
		{name: "rx", text: func(r app.PlanRow) string { return domain.FormatFrequencyMHz(r.Channel.ReceiveHz) }},
		{name: "rxtone", text: func(r app.PlanRow) string {
			_, rx := domain.ProgrammedTones(r.Channel)
			return toneValue(rx)
		}},
		{name: "tx", text: func(r app.PlanRow) string { return mhz(r.Channel.TransmitHz) }},
		{name: "txtone", text: func(r app.PlanRow) string { return toneValue(r.Channel.TransmitTone) }},
		{name: "mode", text: func(r app.PlanRow) string { return r.Channel.Mode }},
		{name: "remarks", text: func(r app.PlanRow) string { return r.Notes }},
		{name: "entry", text: func(r app.PlanRow) string { return strconv.Itoa(r.Entry + 1) }, json: func(r app.PlanRow) any { return r.Entry + 1 }},
		{name: "channel", text: func(r app.PlanRow) string { return r.Channel.Name }},
		{name: "id", id: true, text: func(r app.PlanRow) string { return r.Channel.ID }},
	},
	defaults: []string{"ch", "name", "assignment", "rx", "rxtone", "tx", "txtone", "mode", "remarks"},
}

func newPlanChannelsCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "channels NAME", Short: "List a plan's channels as ICS 205 does", Args: cobra.ExactArgs(1),
		Long: "List a plan's channels as ICS 205 does: plan channel number, name, assignment,\nfrequencies and tones, mode, and remarks. Query sections are expanded."}
	planRowListing.flags(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := planRowListing.check(cmd); err != nil {
			return err
		}
		return withApp(cmd, func(a *app.Service) error {
			p, err := a.GetPlan(ctx(cmd), args[0])
			if err != nil {
				return err
			}
			rows, err := a.PlanRows(ctx(cmd), p)
			if err != nil {
				return err
			}
			return planRowListing.write(cmd, rows)
		})
	}
	return cmd
}

func newPlanUpdateCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "update NAME", Short: "Change a plan; only supplied flags change (maintainer)", Args: cobra.ExactArgs(1)}
	cmd.Flags().String("name", "", "New name")
	planFieldFlags(cmd)
	recordReasonFlag(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return editPlan(cmd, args[0], func(_ *app.Service, p *domain.Plan) error {
			if cmd.Flags().Changed("name") {
				p.Name = getString(cmd, "name")
			}
			return applyPlanFlags(cmd, p)
		})
	}
	return cmd
}

func newPlanArchiveCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "archive NAME", Short: "Archive a plan; radios using it keep its channels (maintainer)", Args: cobra.ExactArgs(1)}
	recordReasonFlag(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return editPlan(cmd, args[0], func(_ *app.Service, p *domain.Plan) error {
			p.Status = domain.PlanArchived
			return nil
		})
	}
	return cmd
}

// editPlan applies fn to a plan and saves it with --reason.
func editPlan(cmd *cobra.Command, ref string, fn func(*app.Service, *domain.Plan) error) error {
	return withApp(cmd, func(a *app.Service) error {
		p, err := a.GetPlan(ctx(cmd), ref)
		if err != nil {
			return err
		}
		if err := fn(a, &p); err != nil {
			return err
		}
		_, err = a.UpdatePlan(ctx(cmd), p, getString(cmd, "reason"))
		return err
	})
}

func planEntryFlags(cmd *cobra.Command) {
	cmd.Flags().String("name", "", "The channel's name in this plan, such as \"FAIR TAC1\" (empty keeps the channel's own)")
	cmd.Flags().String("assignment", "", "What it's used for, such as \"Net control\"")
	cmd.Flags().String("notes", "", "Remarks")
	cmd.Flags().String("section-start", "", "Start the entry at this plan channel number (empty ends it on set)")
}

func applyPlanEntryFlags(cmd *cobra.Command, e *domain.PlanEntry) error {
	for _, f := range []struct {
		name string
		dst  *string
	}{{"name", &e.Name}, {"assignment", &e.Assignment}, {"notes", &e.Notes}} {
		if cmd.Flags().Changed(f.name) {
			*f.dst = getString(cmd, f.name)
		}
	}
	if cmd.Flags().Changed("section-start") {
		v, err := parseSectionStart(getString(cmd, "section-start"))
		if err != nil {
			return err
		}
		e.SectionStart = v
	}
	if cmd.Flags().Changed("sort") {
		e.Sort = getString(cmd, "sort")
	}
	return nil
}

// entryNumber reads an entry number, from 1, within n entries.
func entryNumber(v string, n int) (int, error) {
	i, err := strconv.Atoi(v)
	if err != nil || i < 1 || i > n {
		return 0, fmt.Errorf("entry %q: the plan has entries 1-%d", v, n)
	}
	return i, nil
}

func newPlanEntriesAddCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "add PLAN", Short: "Add shared channels or a query section to a plan (maintainer)", Args: cobra.ExactArgs(1),
		Long: `Add shared channels (--id, one entry each) or a query section (--query) to a
plan, at the end or before entry --at. A query section brings in every
shared channel it matches, in --sort order, leaving out channels the plan
lists elsewhere.

` + stdinIDHelp}
	idsFlag(cmd, "Shared channel IDs, or links' IDs")
	cmd.Flags().StringP("query", "q", "", "A query section instead, such as 'system:\"SBARC Hub\"'")
	cmd.Flags().String("sort", "", "A query section's order: name or freq (default name)")
	cmd.Flags().Int("at", 0, "Insert before this entry number instead of at the end")
	planEntryFlags(cmd)
	recordReasonFlag(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		refs, err := getIDs(cmd)
		if err != nil {
			return err
		}
		q := getString(cmd, "query")
		if (len(refs) > 0) == (q != "") {
			return fmt.Errorf("give --id or --query, not both")
		}
		return editPlan(cmd, args[0], func(a *app.Service, p *domain.Plan) error {
			var add []domain.PlanEntry
			if q != "" {
				e := domain.PlanEntry{Query: q}
				if err := applyPlanEntryFlags(cmd, &e); err != nil {
					return err
				}
				add = append(add, e)
			} else {
				ids, err := a.SharedChannelIDs(ctx(cmd), refs)
				if err != nil {
					return err
				}
				for i, id := range ids {
					e := domain.PlanEntry{ChannelID: id}
					if err := applyPlanEntryFlags(cmd, &e); err != nil {
						return err
					}
					if i > 0 {
						e.SectionStart = nil
					}
					add = append(add, e)
				}
			}
			at := len(p.Entries)
			if v, _ := cmd.Flags().GetInt("at"); cmd.Flags().Changed("at") {
				n, err := entryNumber(strconv.Itoa(v), len(p.Entries))
				if err != nil {
					return err
				}
				at = n - 1
			}
			p.Entries = append(p.Entries[:at:at], append(add, p.Entries[at:]...)...)
			return nil
		})
	}
	return cmd
}

func newPlanEntriesRemoveCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "remove PLAN N", Short: "Remove an entry from a plan (maintainer)", Args: cobra.ExactArgs(2)}
	recordReasonFlag(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return editPlan(cmd, args[0], func(_ *app.Service, p *domain.Plan) error {
			n, err := entryNumber(args[1], len(p.Entries))
			if err != nil {
				return err
			}
			p.Entries = append(p.Entries[:n-1:n-1], p.Entries[n:]...)
			return nil
		})
	}
	return cmd
}

func newPlanEntriesMoveCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "move PLAN N", Short: "Move an entry within a plan (maintainer)", Args: cobra.ExactArgs(2)}
	cmd.Flags().Int("to", 0, "New entry number, from 1 (required)")
	_ = cmd.MarkFlagRequired("to")
	recordReasonFlag(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		to, _ := cmd.Flags().GetInt("to")
		return editPlan(cmd, args[0], func(_ *app.Service, p *domain.Plan) error {
			n, err := entryNumber(args[1], len(p.Entries))
			if err != nil {
				return err
			}
			if to, err = entryNumber(strconv.Itoa(to), len(p.Entries)); err != nil {
				return err
			}
			e := p.Entries[n-1]
			rest := append(p.Entries[:n-1:n-1], p.Entries[n:]...)
			p.Entries = append(rest[:to-1:to-1], append([]domain.PlanEntry{e}, rest[to-1:]...)...)
			return nil
		})
	}
	return cmd
}

func newPlanEntriesSetCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "set PLAN N", Short: "Change an entry; only supplied flags change (maintainer)", Args: cobra.ExactArgs(2)}
	planEntryFlags(cmd)
	cmd.Flags().String("query", "", "A query section's new query")
	cmd.Flags().String("sort", "", "A query section's order: name or freq")
	recordReasonFlag(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return editPlan(cmd, args[0], func(_ *app.Service, p *domain.Plan) error {
			n, err := entryNumber(args[1], len(p.Entries))
			if err != nil {
				return err
			}
			e := &p.Entries[n-1]
			if cmd.Flags().Changed("query") {
				if e.Query == "" {
					return fmt.Errorf("entry %d is a channel, not a query section", n)
				}
				e.Query = getString(cmd, "query")
			}
			return applyPlanEntryFlags(cmd, e)
		})
	}
	return cmd
}
