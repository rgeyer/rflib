package domain

import (
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
)

// Organizations, systems, and plans live in the shared library, beside the
// shared channels, and change only through maintainer operations, each
// recorded as a RecordRevision. They are records of who runs what and how
// it is linked; rflib fetches nothing from the networks they name.

// Network is a voice-over-IP linking network a node can be reached on.
type Network string

const (
	NetworkAllStar  Network = "allstar"
	NetworkEchoLink Network = "echolink"
	NetworkIRLP     Network = "irlp"
	NetworkWiresX   Network = "wires-x"
)

var Networks = []Network{NetworkAllStar, NetworkEchoLink, NetworkIRLP, NetworkWiresX}

// ParseNetwork reads a network's name, ignoring case; wiresx is wires-x.
func ParseNetwork(v string) (Network, error) {
	switch n := Network(strings.ToLower(strings.TrimSpace(v))); n {
	case "wiresx", "wires_x":
		return NetworkWiresX, nil
	case NetworkAllStar, NetworkEchoLink, NetworkIRLP, NetworkWiresX:
		return n, nil
	}
	return "", Invalidf("unknown network %q; use allstar, echolink, irlp, or wires-x", v)
}

// NetworkID is a node's number on a network, such as AllStar node 43763.
type NetworkID struct {
	Network Network `json:"network"`
	Node    string  `json:"node"`
}

func (n NetworkID) String() string { return string(n.Network) + ":" + n.Node }

// ParseNetworkID reads NETWORK:NODE, such as allstar:43763. Nodes are
// numbers.
func ParseNetworkID(v string) (NetworkID, error) {
	name, node, ok := strings.Cut(strings.TrimSpace(v), ":")
	if !ok {
		return NetworkID{}, Invalidf("%q: use NETWORK:NODE, such as allstar:43763", v)
	}
	net, err := ParseNetwork(name)
	if err != nil {
		return NetworkID{}, err
	}
	node = strings.TrimSpace(node)
	if node == "" || len(node) > 8 || strings.Trim(node, "0123456789") != "" {
		return NetworkID{}, Invalidf("%q: a %s node is a number", v, net)
	}
	return NetworkID{Network: net, Node: node}, nil
}

// ParseNetworkIDs reads a list of NETWORK:NODE values.
func ParseNetworkIDs(vs []string) ([]NetworkID, error) {
	var out []NetworkID
	for _, v := range vs {
		if strings.TrimSpace(v) == "" {
			continue
		}
		n, err := ParseNetworkID(v)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return NormalizeNetworkIDs(out), nil
}

// NormalizeNetworkIDs sorts network IDs and drops duplicates.
func NormalizeNetworkIDs(ns []NetworkID) []NetworkID {
	out := slices.Clone(ns)
	slices.SortFunc(out, func(a, b NetworkID) int {
		return cmp.Or(cmp.Compare(a.Network, b.Network), CompareNames(a.Node, b.Node))
	})
	return slices.Compact(out)
}

// URL is a page about the node on its network's own site: an AllStar
// node's statistics, or the network's node lookup where it has no page per
// node.
func (n NetworkID) URL() string {
	switch n.Network {
	case NetworkAllStar:
		return "https://stats.allstarlink.org/stats/" + n.Node
	case NetworkEchoLink:
		return "https://www.echolink.org/validation/node_lookup.jsp"
	case NetworkIRLP:
		return "http://status.irlp.net/"
	}
	return ""
}

func formatNetworkIDs(ns []NetworkID) string {
	s := make([]string, len(ns))
	for i, n := range ns {
		s[i] = n.String()
	}
	return strings.Join(s, ",")
}

// OrgKind is what sort of organization one is.
type OrgKind string

const (
	OrgClub       OrgKind = "club"
	OrgEmergency  OrgKind = "emergency"
	OrgAgency     OrgKind = "agency"
	OrgIndividual OrgKind = "individual"
)

var OrgKinds = []OrgKind{OrgClub, OrgEmergency, OrgAgency, OrgIndividual}

func ParseOrgKind(v string) (OrgKind, error) {
	switch k := OrgKind(strings.ToLower(strings.TrimSpace(v))); k {
	case "":
		return OrgClub, nil
	case OrgClub, OrgEmergency, OrgAgency, OrgIndividual:
		return k, nil
	}
	return "", Invalidf("unknown organization kind %q; use club, emergency, agency, or individual", v)
}

// Organization is who operates or supports repeaters and systems: a club,
// an emergency service, an agency, or a person. It is a plain record, with
// no say over what's attached to it.
type Organization struct {
	ID       string  `json:"-"`
	Name     string  `json:"name"`
	Kind     OrgKind `json:"kind"`
	Callsign string  `json:"callsign,omitempty"`
	Website  string  `json:"website,omitempty"`
	Notes    string  `json:"notes,omitempty"`
	// Revision counts its changes (see RecordRevision).
	Revision  int       `json:"-"`
	CreatedAt time.Time `json:"-"`
	UpdatedAt time.Time `json:"-"`
}

func ValidateOrganization(o Organization) error {
	if strings.TrimSpace(o.Name) == "" {
		return Invalidf("organization name is required")
	}
	if _, err := ParseOrgKind(string(o.Kind)); err != nil {
		return err
	}
	if o.Website != "" && !strings.HasPrefix(o.Website, "https://") && !strings.HasPrefix(o.Website, "http://") {
		return Invalidf("website must start with https:// or http://")
	}
	return nil
}

// OrgRole is an organization's part in a channel or system.
type OrgRole string

const (
	// RoleOperator runs it.
	RoleOperator OrgRole = "operator"
	// RolePartner sponsors or supports it, such as a site's co-sponsor.
	RolePartner OrgRole = "partner"
)

func ParseOrgRole(v string) (OrgRole, error) {
	switch r := OrgRole(strings.ToLower(strings.TrimSpace(v))); r {
	case "":
		return RoleOperator, nil
	case RoleOperator, RolePartner:
		return r, nil
	}
	return "", Invalidf("unknown role %q; use operator or partner", v)
}

// OrgRef attaches an organization to a channel or system. Name is filled
// in on reads.
type OrgRef struct {
	OrgID string  `json:"org_id"`
	Name  string  `json:"name"`
	Role  OrgRole `json:"role"`
}

// NormalizeOrgRefs sorts operators first, then by name, and drops
// duplicates of the same organization in the same role.
func NormalizeOrgRefs(rs []OrgRef) []OrgRef {
	out := slices.Clone(rs)
	for i := range out {
		out[i].Role, _ = ParseOrgRole(string(out[i].Role))
	}
	slices.SortFunc(out, func(a, b OrgRef) int {
		return cmp.Or(cmp.Compare(roleRank(a.Role), roleRank(b.Role)), CompareNames(a.Name, b.Name), cmp.Compare(a.OrgID, b.OrgID))
	})
	return slices.CompactFunc(out, func(a, b OrgRef) bool { return a.OrgID == b.OrgID && a.Role == b.Role })
}

func roleRank(r OrgRole) int {
	if r == RoleOperator {
		return 0
	}
	return 1
}

func formatOrgRefs(rs []OrgRef) string {
	s := make([]string, len(rs))
	for i, r := range rs {
		s[i] = string(r.Role) + " " + cmp.Or(r.Name, r.OrgID)
	}
	return strings.Join(s, ", ")
}

// LinkType is how a system's members are tied together.
type LinkType string

var LinkTypes = []LinkType{"allstar", "echolink", "irlp", "wires-x", "rf", "other"}

func ParseLinkType(v string) (LinkType, error) {
	t := LinkType(strings.ToLower(strings.TrimSpace(v)))
	if t == "wiresx" {
		t = "wires-x"
	}
	if t == "" || slices.Contains(LinkTypes, t) {
		return t, nil
	}
	return "", Invalidf("unknown link type %q; use allstar, echolink, irlp, wires-x, rf, or other", v)
}

// Linking is how a member takes part in its system.
type Linking string

const (
	// LinkFullTime members are usually linked.
	LinkFullTime Linking = "full-time"
	// LinkOnDemand members may be linked, such as for a net.
	LinkOnDemand Linking = "on-demand"
)

func ParseLinking(v string) (Linking, error) {
	switch l := Linking(strings.ToLower(strings.TrimSpace(v))); l {
	case "", "fulltime", "full_time":
		return LinkFullTime, nil
	case "ondemand", "on_demand":
		return LinkOnDemand, nil
	case LinkFullTime, LinkOnDemand:
		return l, nil
	}
	return "", Invalidf("unknown linking %q; use full-time or on-demand", v)
}

// System is a set of interconnected channels, such as a club's linked
// repeaters, as its operators intend it: not its live connections. It can
// be reachable on networks of its own, such as a hub's AllStar node, and
// its members can belong to different organizations.
type System struct {
	ID        string         `json:"-"`
	Name      string         `json:"name"`
	Link      LinkType       `json:"link,omitempty"`
	Networks  []NetworkID    `json:"networks,omitempty"`
	Orgs      []OrgRef       `json:"organizations,omitempty"`
	Members   []SystemMember `json:"members,omitempty"`
	Notes     string         `json:"notes,omitempty"`
	Revision  int            `json:"-"`
	CreatedAt time.Time      `json:"-"`
	UpdatedAt time.Time      `json:"-"`
}

// SystemMember is a shared channel in a system.
type SystemMember struct {
	ChannelID string  `json:"channel_id"`
	Linking   Linking `json:"linking"`
	// Notes say how it's linked, such as "microwave to the hub".
	Notes string `json:"notes,omitempty"`
}

func ValidateSystem(s System) error {
	if strings.TrimSpace(s.Name) == "" {
		return Invalidf("system name is required")
	}
	if _, err := ParseLinkType(string(s.Link)); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, m := range s.Members {
		if seen[m.ChannelID] {
			return Invalidf("channel %s is in the system twice", m.ChannelID)
		}
		seen[m.ChannelID] = true
		if _, err := ParseLinking(string(m.Linking)); err != nil {
			return err
		}
	}
	return nil
}

// PlanStatus is where a plan is in its life.
type PlanStatus string

const (
	PlanDraft     PlanStatus = "draft"
	PlanPublished PlanStatus = "published"
	// PlanArchived plans are kept, never deleted, but no longer used.
	PlanArchived PlanStatus = "archived"
)

func ParsePlanStatus(v string) (PlanStatus, error) {
	switch s := PlanStatus(strings.ToLower(strings.TrimSpace(v))); s {
	case "":
		return PlanDraft, nil
	case PlanDraft, PlanPublished, PlanArchived:
		return s, nil
	}
	return "", Invalidf("unknown plan status %q; use draft, published, or archived", v)
}

// Plan is a communications plan: an ordered selection of shared channels,
// each with its name and assignment in the plan, laid out as ICS 205
// lists them. An event is a plan with dates. Radios use a plan as a
// source.
type Plan struct {
	ID          string      `json:"-"`
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	Starts      *time.Time  `json:"starts,omitempty"`
	Ends        *time.Time  `json:"ends,omitempty"`
	Status      PlanStatus  `json:"status"`
	Entries     []PlanEntry `json:"entries,omitempty"`
	Revision    int         `json:"-"`
	CreatedAt   time.Time   `json:"-"`
	UpdatedAt   time.Time   `json:"-"`
}

// PlanEntry is one line of a plan: a shared channel, or a query section
// that brings in every shared channel it matches.
type PlanEntry struct {
	// ChannelID is the shared channel; empty for a query section.
	ChannelID string `json:"channel_id,omitempty"`
	// Query and Sort make a query section.
	Query string `json:"query,omitempty"`
	Sort  string `json:"sort,omitempty"`
	// Name is the channel's name in this plan, such as "FAIR TAC1" for a
	// simplex frequency; empty keeps the channel's own.
	Name       string `json:"name,omitempty"`
	Assignment string `json:"assignment,omitempty"`
	Notes      string `json:"notes,omitempty"`
	// SectionStart starts the entry at that plan channel number (ICS 205's
	// Ch #); the entries after it continue from there.
	SectionStart *int `json:"section_start,omitempty"`
}

func ValidatePlan(p Plan) error {
	if strings.TrimSpace(p.Name) == "" {
		return Invalidf("plan name is required")
	}
	if _, err := ParsePlanStatus(string(p.Status)); err != nil {
		return err
	}
	if p.Starts != nil && p.Ends != nil && p.Ends.Before(*p.Starts) {
		return Invalidf("a plan can't end before it starts")
	}
	for i, e := range p.Entries {
		if (e.ChannelID == "") == (strings.TrimSpace(e.Query) == "") {
			return Invalidf("entry %d needs a channel or a query, not both", i+1)
		}
		if e.Query != "" && e.Name != "" {
			return Invalidf("entry %d is a query section, so it can't rename its channels", i+1)
		}
		if e.Query != "" && e.Sort != "" && !slices.Contains(SortOrders, e.Sort) {
			return sortError(e.Sort)
		}
		if e.SectionStart != nil && *e.SectionStart < 1 {
			return Invalidf("entry %d: plan channel numbers start at 1", i+1)
		}
	}
	return nil
}

// RecordRevision is one version of an organization, system, or plan: the
// record as it was then, and why it changed.
type RecordRevision struct {
	Number int
	At     time.Time
	Reason string
	Author string
	// Record is the record's fields, as JSON.
	Record json.RawMessage
}

// Snapshot is a record's fields as its revisions store them.
func Snapshot(v any) (json.RawMessage, error) { return json.Marshal(v) }

// RecordChanges lists how two snapshots differ, one "field: old → new"
// line per top-level field.
func RecordChanges(a, b json.RawMessage) []string {
	var x, y map[string]json.RawMessage
	_ = json.Unmarshal(a, &x)
	_ = json.Unmarshal(b, &y)
	var keys []string
	for k := range x {
		keys = append(keys, k)
	}
	for k := range y {
		if _, ok := x[k]; !ok {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	var out []string
	for _, k := range keys {
		if bytes.Equal(x[k], y[k]) {
			continue
		}
		field := strings.ReplaceAll(k, "_", " ")
		if line, ok := listChange(x[k], y[k]); ok {
			out = append(out, field+": "+line)
			continue
		}
		out = append(out, fmt.Sprintf("%s: %s → %s", field, showJSON(x[k]), showJSON(y[k])))
	}
	return out
}

// listChange summarizes a change to a list, such as a system's members:
// how many there were and are, and how many were added, removed, changed,
// or moved. It reports false when either value isn't a list.
func listChange(a, b json.RawMessage) (string, bool) {
	var x, y []json.RawMessage
	if (len(a) > 0 && json.Unmarshal(a, &x) != nil) || (len(b) > 0 && json.Unmarshal(b, &y) != nil) {
		return "", false
	}
	count := func(xs []json.RawMessage) map[string]int {
		m := map[string]int{}
		for _, v := range xs {
			m[string(v)]++
		}
		return m
	}
	cx, cy := count(x), count(y)
	added, removed := 0, 0
	for k, n := range cy {
		added += max(0, n-cx[k])
	}
	for k, n := range cx {
		removed += max(0, n-cy[k])
	}
	var parts []string
	switch {
	case added > 0 && added == removed:
		parts = append(parts, fmt.Sprintf("%d changed", added))
	default:
		if added > 0 {
			parts = append(parts, fmt.Sprintf("%d added", added))
		}
		if removed > 0 {
			parts = append(parts, fmt.Sprintf("%d removed", removed))
		}
	}
	if len(parts) == 0 {
		parts = append(parts, "reordered")
	}
	return fmt.Sprintf("%d → %d (%s)", len(x), len(y), strings.Join(parts, ", ")), true
}

func showJSON(v json.RawMessage) string {
	if len(v) == 0 {
		return "(none)"
	}
	var s string
	if json.Unmarshal(v, &s) == nil {
		return orNone(s)
	}
	return string(v)
}
