package api

import (
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ryan/rflib/internal/app"
	"github.com/ryan/rflib/internal/domain"
	"github.com/ryan/rflib/internal/model"
	"github.com/ryan/rflib/internal/program"
	"github.com/ryan/rflib/internal/query"
)

// The API's JSON shapes. Frequencies are exact integer Hz; tones use the
// library's syntax: none, ctcss:100.000, dcs:023, dcs:023:inverted. Fields
// that can be absent are null rather than missing.

type Location struct {
	Latitude  float64  `json:"latitude"`
	Longitude float64  `json:"longitude"`
	RadiusKM  *float64 `json:"radius_km"`
}

type Link struct {
	SharedID     string   `json:"shared_id"`
	Tags         []string `json:"tags"`
	Notes        string   `json:"notes"`
	SeenRevision int      `json:"seen_revision"`
	Changed      bool     `json:"changed"`
}

type Channel struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	RxHz        int64     `json:"rx_hz"`
	TxHz        *int64    `json:"tx_hz"`
	Mode        string    `json:"mode"`
	Service     string    `json:"service"`
	RxOnly      bool      `json:"rx_only"`
	RxTone      string    `json:"rx_tone"`
	TxTone      string    `json:"tx_tone"`
	ToneSquelch bool      `json:"tone_squelch"`
	Power       string    `json:"power"`
	Bandwidth   string    `json:"bandwidth"`
	Skip        bool      `json:"skip"`
	Tags        []string  `json:"tags"`
	ShortNames  []string  `json:"short_names"`
	Notes       string    `json:"notes"`
	Location    *Location `json:"location"`
	Status      string    `json:"status"`
	StatusNote  string    `json:"status_note"`
	BackOn      *string   `json:"back_on"`
	ReplacedBy  *string   `json:"replaced_by"`
	Revision    int       `json:"revision"`
	CopiedFrom  *string   `json:"copied_from"`
	Link        *Link     `json:"link"`
	Networks    []string  `json:"networks"`
	Orgs        []OrgRef  `json:"organizations"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func nonNil[T any](v []T) []T {
	if v == nil {
		return []T{}
	}
	return v
}

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func newChannel(c domain.Channel) Channel {
	o := Channel{
		ID: c.ID, Name: c.Name, RxHz: c.ReceiveHz, TxHz: c.TransmitHz, Mode: c.Mode, Service: string(c.Service), RxOnly: c.RXOnly,
		RxTone: domain.FormatTone(c.ReceiveTone), TxTone: domain.FormatTone(c.TransmitTone), ToneSquelch: c.ToneSquelch,
		Power: string(c.Power), Bandwidth: c.Bandwidth, Skip: c.Skip, Tags: nonNil(c.Tags), ShortNames: nonNil(c.ShortNames), Notes: c.Notes,
		Status: string(c.Status.OrActive()), StatusNote: c.StatusNote, ReplacedBy: optional(c.ReplacedBy), Revision: c.Revision,
		CopiedFrom: optional(c.CopiedFrom), CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
	}
	if l := c.Location; l != nil {
		o.Location = &Location{l.Latitude, l.Longitude, l.RadiusKM}
	}
	if c.BackOn != nil {
		o.BackOn = optional(c.BackOn.Format(time.DateOnly))
	}
	if l := c.Link; l != nil {
		o.Link = &Link{SharedID: l.SharedID, Tags: nonNil(l.Tags), Notes: l.Notes, SeenRevision: l.SeenRevision, Changed: c.Changed()}
	}
	o.Networks, o.Orgs = networkStrings(c.Networks), newOrgRefs(c.Orgs)
	return o
}

func newChannels(cs []domain.Channel) []Channel {
	out := make([]Channel, len(cs))
	for i, c := range cs {
		out[i] = newChannel(c)
	}
	return out
}

// channelFields are the fields a request may set on a channel.
var channelFields = []string{"name", "rx_hz", "tx_hz", "mode", "service", "rx_only", "rx_tone", "tx_tone", "tone_squelch",
	"power", "bandwidth", "skip", "tags", "short_names", "notes", "location", "networks", "organizations"}

// applyChannel sets the fields present in raw on c. Absent fields are left
// alone; a null clears tx_hz and location. A service turns rx_only on when
// listen-only, unless rx_only is given too.
func applyChannel(c *domain.Channel, raw map[string]json.RawMessage) error {
	for k := range raw {
		if !slices.Contains(channelFields, k) {
			return badRequest("unknown channel field %q; settable fields are %s", k, strings.Join(channelFields, ", "))
		}
	}
	get := func(key string, v any) error {
		if err := json.Unmarshal(raw[key], v); err != nil {
			return badRequest("%s: %v", key, err)
		}
		return nil
	}
	str := func(key string, set func(string) error) error {
		if _, ok := raw[key]; !ok {
			return nil
		}
		var v string
		if err := get(key, &v); err != nil {
			return err
		}
		return set(v)
	}
	boolean := func(key string, dst *bool) error {
		if _, ok := raw[key]; !ok {
			return nil
		}
		return get(key, dst)
	}
	list := func(key string, dst *[]string) error {
		if _, ok := raw[key]; !ok {
			return nil
		}
		return get(key, dst)
	}
	steps := []error{
		str("name", func(v string) error { c.Name = v; return nil }),
		str("mode", func(v string) error { c.Mode = v; return nil }),
		str("bandwidth", func(v string) error { c.Bandwidth = v; return nil }),
		str("notes", func(v string) error { c.Notes = v; return nil }),
		str("power", func(v string) (err error) { c.Power, err = domain.ParsePowerIntent(v); return }),
		str("rx_tone", func(v string) (err error) { c.ReceiveTone, err = domain.ParseTone(v); return }),
		str("tx_tone", func(v string) (err error) { c.TransmitTone, err = domain.ParseTone(v); return }),
		str("service", func(v string) error {
			svc, err := domain.ParseService(v)
			c.Service = svc
			if _, given := raw["rx_only"]; !given {
				c.RXOnly = c.RXOnly || domain.DefaultRXOnly(svc)
			}
			return err
		}),
		boolean("rx_only", &c.RXOnly), boolean("tone_squelch", &c.ToneSquelch), boolean("skip", &c.Skip),
		list("tags", &c.Tags), list("short_names", &c.ShortNames),
	}
	for _, err := range steps {
		if err != nil {
			return err
		}
	}
	if v, ok := raw["rx_hz"]; ok {
		if err := json.Unmarshal(v, &c.ReceiveHz); err != nil {
			return badRequest("rx_hz: %v", err)
		}
	}
	if v, ok := raw["tx_hz"]; ok {
		c.TransmitHz = nil
		if !isNull(v) {
			var hz int64
			if err := json.Unmarshal(v, &hz); err != nil {
				return badRequest("tx_hz: %v", err)
			}
			c.TransmitHz = &hz
		}
	}
	if v, ok := raw["location"]; ok {
		loc, err := decodeLocation(v)
		if err != nil {
			return err
		}
		c.Location = loc
	}
	if _, ok := raw["networks"]; ok {
		var vs []string
		if err := get("networks", &vs); err != nil {
			return err
		}
		ns, err := domain.ParseNetworkIDs(vs)
		if err != nil {
			return err
		}
		c.Networks = ns
	}
	if _, ok := raw["organizations"]; ok {
		var in []orgRefInput
		if err := get("organizations", &in); err != nil {
			return err
		}
		rs, err := appOrgRefs(in)
		if err != nil {
			return err
		}
		c.Orgs = rs
	}
	return nil
}

func isNull(v json.RawMessage) bool { return bytes.Equal(bytes.TrimSpace(v), []byte("null")) }

func decodeLocation(v json.RawMessage) (*domain.Location, error) {
	if isNull(v) {
		return nil, nil
	}
	var l struct {
		Latitude  *float64 `json:"latitude"`
		Longitude *float64 `json:"longitude"`
		RadiusKM  *float64 `json:"radius_km"`
	}
	dec := json.NewDecoder(bytes.NewReader(v))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&l); err != nil {
		return nil, badRequest("location: %v", err)
	}
	if l.Latitude == nil || l.Longitude == nil {
		return nil, badRequest("location needs latitude and longitude")
	}
	return domain.NewLocation(l.Latitude, l.Longitude, l.RadiusKM)
}

// Selection picks channels: by ID (or unique prefix), by query, or all.
// With both IDs and a query, a channel must match both.
type Selection struct {
	IDs   []string `json:"ids"`
	Query string   `json:"query"`
	All   bool     `json:"all"`
}

func (s Selection) app() (app.Selection, error) {
	sel := app.Selection{IDs: s.IDs, All: s.All}
	if strings.TrimSpace(s.Query) != "" {
		n, err := query.Parse(s.Query)
		if err != nil {
			return sel, err
		}
		sel.Query = n
	}
	if sel.All && (len(sel.IDs) > 0 || sel.Query != nil) {
		return sel, badRequest("all can't be combined with ids or query")
	}
	return sel, nil
}

// TagEdit sets tags, or adds and removes them.
type TagEdit struct {
	Set    []string `json:"set"`
	Add    []string `json:"add"`
	Remove []string `json:"remove"`
}

// ChannelEdit is a change to many channels at once: only fields that make
// sense in bulk. Absent fields are left alone; a null location clears it.
type ChannelEdit struct {
	Tags     *TagEdit        `json:"tags"`
	Skip     *bool           `json:"skip"`
	Power    *string         `json:"power"`
	Service  *string         `json:"service"`
	RxOnly   *bool           `json:"rx_only"`
	Location json.RawMessage `json:"location"`
}

func (e ChannelEdit) app() (app.ChannelEdit, error) {
	out := app.ChannelEdit{Skip: e.Skip, RXOnly: e.RxOnly}
	if e.Tags != nil {
		out.Tags = &app.TagEdit{Set: e.Tags.Set, Add: e.Tags.Add, Remove: e.Tags.Remove}
	}
	if e.Power != nil {
		p, err := domain.ParsePowerIntent(*e.Power)
		if err != nil {
			return out, err
		}
		out.Power = &p
	}
	if e.Service != nil {
		svc, err := domain.ParseService(*e.Service)
		if err != nil {
			return out, err
		}
		out.Service = &svc
	}
	if e.Location != nil {
		loc, err := decodeLocation(e.Location)
		if err != nil {
			return out, err
		}
		out.SetLocation, out.Location = true, loc
	}
	if out == (app.ChannelEdit{}) {
		return out, badRequest("nothing to change; give tags, skip, power, service, rx_only, or location")
	}
	return out, nil
}

type Library struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Shared      bool      `json:"shared"`
	Default     bool      `json:"default"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func newLibrary(l domain.Library) Library {
	return Library{l.ID, l.Name, l.Description, l.Shared, l.Default, l.CreatedAt, l.UpdatedAt}
}

type Radio struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Model        string    `json:"model"`
	DefaultPower string    `json:"default_power"`
	Dedupe       string    `json:"dedupe"`
	NameLength   int       `json:"name_length"`
	Notes        string    `json:"notes"`
	Channels     int       `json:"channels"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func newRadio(r domain.Radio, channels int) Radio {
	return Radio{r.ID, r.Name, r.Model, string(r.DefaultPower), cmpOr(r.Dedupe, "none"), r.NameLength, r.Notes, channels, r.CreatedAt, r.UpdatedAt}
}

func cmpOr(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

type Source struct {
	Number      int     `json:"number"`
	ID          string  `json:"id"`
	Query       string  `json:"query"`
	Plan        *string `json:"plan"`
	PlanID      *string `json:"plan_id"`
	Sort        string  `json:"sort"`
	SectionID   string  `json:"section_id"`
	Section     string  `json:"section"`
	PlanNumbers string  `json:"plan_numbers"`
	Channels    int     `json:"channels"`
}

func newSources(srcs []domain.RadioSource, es []domain.RadioEntry) []Source {
	count := map[string]int{}
	for _, e := range es {
		count[e.SourceID]++
	}
	out := make([]Source, len(srcs))
	for i, s := range srcs {
		out[i] = Source{s.Position + 1, s.ID, s.Query, optional(s.PlanName), optional(s.PlanID), s.Sort, s.SectionID, s.SectionName, s.PlanNumbers, count[s.ID]}
	}
	return out
}

// Overrides says which of a memory's settings the radio overrides.
type Overrides struct {
	Name  bool `json:"name"`
	Power bool `json:"power"`
	Scan  bool `json:"scan"`
}

// Memory is one memory as a radio will be programmed.
type Memory struct {
	Memory  *int   `json:"memory"`
	Section string `json:"section"`
	// PlanNumber is a plan source's Ch #.
	PlanNumber *int `json:"plan_number"`
	// Source is the number of the source that added it; null when added
	// by hand.
	Source *int `json:"source"`
	// Overflow: past the end of its section's size.
	Overflow bool    `json:"overflow"`
	Name     string  `json:"name"`
	Fit      string  `json:"fit"`
	Deduped  bool    `json:"deduped"`
	Power    *string `json:"power"`
	Scan     bool    `json:"scan"`
	Exported bool    `json:"exported"`
	// NotProgrammed says why an export doesn't program the memory.
	NotProgrammed *string `json:"not_programmed"`
	ReceiveOnly   bool    `json:"receive_only"`
	// TransmitToneOnly: the radio can't hold the receive tone.
	TransmitToneOnly bool      `json:"transmit_tone_only"`
	Overrides        Overrides `json:"overrides"`
	Channel          Channel   `json:"channel"`
}

func newMemories(p program.Layout, es []domain.RadioEntry) []Memory {
	out := make([]Memory, len(p.Memories))
	for i, m := range p.Memories {
		e := es[i]
		var power *string
		if m.Channel.ProgrammedPower != nil {
			power = &m.Channel.ProgrammedPower.Label
		}
		var why *string
		if r := m.NotProgrammed(); r != "" {
			why = &r
		}
		var source *int
		if e.SourceNumber > 0 {
			n := e.SourceNumber
			source = &n
		}
		out[i] = Memory{number(m.Location), e.Section.ID, e.PlanNumber, source, m.Overflow, m.Channel.Name, string(m.Name.Method), m.Name.Deduped, power, !m.Channel.Skip, m.Programmed(),
			why, m.ReceiveOnly(), m.TransmitToneOnly, Overrides{e.Name != "", e.Power != "", e.Skip != nil}, newChannel(m.Original)}
	}
	return out
}

// Section is one of a radio's sections, as laid out: First is the memory
// it begins at, and Last the last its channels use (First - 1 for none).
type Section struct {
	ID      string `json:"id"`
	Number  int    `json:"number"`
	Name    string `json:"name"`
	Start   *int   `json:"start"`
	Size    *int   `json:"size"`
	Sort    string `json:"sort"`
	Default bool   `json:"default"`
	First   int    `json:"first"`
	Last    int    `json:"last"`
}

func newSections(p program.Layout) []Section {
	out := make([]Section, len(p.Sections))
	for i, x := range p.Sections {
		sec := x.Section
		out[i] = Section{sec.ID, sec.Position + 1, sec.Name, sec.Start, sec.Size, cmp.Or(sec.Sort, "manual"), sec.Default, x.First, x.Last}
	}
	return out
}

type Search struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Query   string   `json:"query"`
	Notes   string   `json:"notes"`
	Matches *int     `json:"matches"`
	Error   *string  `json:"error"`
	UsedBy  []string `json:"used_by,omitempty"`
}

func newSearch(x app.SearchSummary) Search {
	o := Search{ID: x.ID, Name: x.Name, Query: x.Query, Notes: x.Notes}
	if x.Err != nil {
		o.Error = optional(x.Err.Error())
	} else {
		n := x.Matches
		o.Matches = &n
	}
	return o
}

type Revision struct {
	Revision int       `json:"revision"`
	At       time.Time `json:"at"`
	Reason   string    `json:"reason"`
	Author   string    `json:"author"`
	Changes  []string  `json:"changes"`
	Channel  Channel   `json:"channel"`
}

func newRevisions(revs []domain.Revision) []Revision {
	out := make([]Revision, len(revs))
	for i, r := range revs {
		var changes []string
		if i > 0 {
			changes = domain.SharedChanges(revs[i-1].Channel, r.Channel)
		}
		out[i] = Revision{r.Number, r.At, r.Reason, r.Author, nonNil(changes), newChannel(r.Channel)}
	}
	return out
}

type Change struct {
	Channel     Channel  `json:"channel"`
	Changes     []string `json:"changes"`
	Reasons     []string `json:"reasons"`
	Radios      []string `json:"radios"`
	Replacement *Channel `json:"replacement"`
}

func newChange(c app.Change) Change {
	o := Change{newChannel(c.Channel), nonNil(c.Changes), nonNil(c.Reasons), nonNil(c.Radios), nil}
	if c.Replacement != nil {
		r := newChannel(*c.Replacement)
		o.Replacement = &r
	}
	return o
}

type Conversion struct {
	Channel Channel `json:"channel"`
	Shared  Channel `json:"shared"`
	Link    Channel `json:"link"`
}

type Model struct {
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Format      string       `json:"format"`
	ChirpModel  string       `json:"chirp_model"`
	Memories    [2]int       `json:"memories"`
	NameLength  int          `json:"name_length"`
	Modes       []string     `json:"modes"`
	BandsMHz    [][2]float64 `json:"bands_mhz"`
	Power       []PowerLevel `json:"power"`
	Notes       string       `json:"notes"`
	Source      string       `json:"source"`
}

type PowerLevel struct {
	Label  string   `json:"label"`
	Watts  *float64 `json:"watts"`
	Intent string   `json:"intent"`
}

func newModel(m model.Model) Model {
	o := Model{Name: m.Name, Description: m.Description, Format: m.Format, ChirpModel: m.ChirpModel, Memories: [2]int{m.Memories.First, m.Memories.Last},
		NameLength: m.Names.MaxLength, Modes: nonNil(m.Modes), BandsMHz: nonNil(m.BandsMHz), Power: []PowerLevel{}, Notes: m.Notes, Source: m.Source}
	for _, l := range m.Power {
		o.Power = append(o.Power, PowerLevel{l.Label, l.Watts, string(l.Intent)})
	}
	return o
}

// date parses an optional YYYY-MM-DD.
func date(v *string) (*time.Time, error) {
	if v == nil || *v == "" {
		return nil, nil
	}
	t, err := time.Parse(time.DateOnly, *v)
	if err != nil {
		return nil, badRequest("dates are YYYY-MM-DD: %q", *v)
	}
	return &t, nil
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}
