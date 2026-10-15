package query

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ryan/rflib/internal/domain"
)

// Matcher reports whether a channel matches a compiled query.
type Matcher func(domain.Channel) bool

// Env supplies what a query can refer to beyond the channel itself. A nil
// function makes the terms that need it an error.
type Env struct {
	// Search returns a saved search's query text.
	Search func(name string) (string, error)
	// RadioChannels returns the IDs of the channels on a radio (by ID or
	// name), or on any radio when ref is empty.
	RadioChannels func(ref string) (map[string]bool, error)
	// Tunes reports whether a radio (by ID or name) or radio model can
	// receive a channel: its frequency and its mode.
	Tunes func(ref string) (func(domain.Channel) bool, error)
	// System returns the IDs of a system's channels (by system ID or
	// name), which are shared channels: a link matches through its shared
	// channel.
	System func(ref string) (map[string]bool, error)
}

// Compile turns a query into a Matcher, resolving saved searches,
// and radios through env.
func Compile(n Node, env Env) (Matcher, error) {
	c := compiler{env: env}
	return c.node(n)
}

// CompileText parses and compiles query text.
func CompileText(text string, env Env) (Matcher, error) {
	n, err := Parse(text)
	if err != nil {
		return nil, err
	}
	return Compile(n, env)
}

type compiler struct {
	env Env
	// using is the chain of saved searches being expanded, to catch cycles.
	using []string
}

func (c *compiler) node(n Node) (Matcher, error) {
	switch x := n.(type) {
	case And:
		ms, err := c.nodes(x.Nodes)
		if err != nil {
			return nil, err
		}
		return func(ch domain.Channel) bool {
			for _, m := range ms {
				if !m(ch) {
					return false
				}
			}
			return true
		}, nil
	case Or:
		ms, err := c.nodes(x.Nodes)
		if err != nil {
			return nil, err
		}
		return func(ch domain.Channel) bool {
			for _, m := range ms {
				if m(ch) {
					return true
				}
			}
			return false
		}, nil
	case Not:
		m, err := c.node(x.Node)
		if err != nil {
			return nil, err
		}
		return func(ch domain.Channel) bool { return !m(ch) }, nil
	case Text:
		return textMatcher(x.Value), nil
	case Ref:
		return c.ref(x)
	case Term:
		m, err := c.term(x)
		if err != nil {
			var qe *Error
			if errors.As(err, &qe) {
				return nil, err
			}
			return nil, errorAt(x.Pos, "%s: %v", x.Field, err)
		}
		return m, nil
	}
	return nil, domain.Invalidf("query: unknown node %T", n)
}

func (c *compiler) nodes(ns []Node) ([]Matcher, error) {
	ms := make([]Matcher, len(ns))
	for i, n := range ns {
		m, err := c.node(n)
		if err != nil {
			return nil, err
		}
		ms[i] = m
	}
	return ms, nil
}

func (c *compiler) ref(r Ref) (Matcher, error) {
	if c.env.Search == nil {
		return nil, errorAt(r.Pos, "saved searches can't be used here")
	}
	if i := slices.Index(c.using, r.Name); i >= 0 {
		chain := append(slices.Clone(c.using[i:]), r.Name)
		return nil, &cycleError{fmt.Sprintf("query: saved search @%s uses itself (%s)", r.Name, "@"+strings.Join(chain, " → @"))}
	}
	text, err := c.env.Search(r.Name)
	if err != nil {
		return nil, errorAt(r.Pos, "@%s: %v", r.Name, err)
	}
	n, err := Parse(text)
	if err != nil {
		return nil, fmt.Errorf("saved search @%s: %w", r.Name, err)
	}
	c.using = append(c.using, r.Name)
	defer func() { c.using = c.using[:len(c.using)-1] }()
	m, err := c.node(n)
	var cycle *cycleError
	if err != nil && len(c.using) == 1 && !errors.As(err, &cycle) {
		return nil, fmt.Errorf("saved search @%s: %w", r.Name, err)
	}
	return m, err
}

// cycleError is a saved search that uses itself. Its message names the
// whole chain, so it isn't wrapped with the search it was found through.
type cycleError struct{ msg string }

func (e *cycleError) Error() string { return e.msg }

// Fields are the term fields, in the order help lists them.
var Fields = []string{
	"name", "notes", "service", "tag", "group", "freq", "rx", "tx", "band", "duplex",
	"tone", "txtone", "rxtone", "mode", "power", "squelch", "skip", "is", "has",
	"near", "radio", "tunes", "created", "updated", "id", "status", "in",
	"org", "system", "allstar", "echolink", "irlp", "wires-x",
}

var fieldAliases = map[string]string{"frequency": "freq", "tags": "tag", "svc": "service", "organization": "org", "wiresx": "wires-x"}

func (c *compiler) term(t Term) (Matcher, error) {
	switch t.Field {
	case "freq", "rx", "tx", "created", "updated":
	default:
		if t.Op != "" {
			return nil, domain.Invalidf("doesn't take %s; comparisons work on freq, rx, tx, created, and updated", t.Op)
		}
	}
	v := t.Value
	switch t.Field {
	case "name":
		v = strings.ToLower(v)
		return func(ch domain.Channel) bool {
			return slices.ContainsFunc(append([]string{ch.Name}, ch.ShortNames...), func(s string) bool { return strings.Contains(strings.ToLower(s), v) })
		}, nil
	case "notes":
		v = strings.ToLower(v)
		return func(ch domain.Channel) bool { return strings.Contains(strings.ToLower(ch.AllNotes()), v) }, nil
	case "service":
		svc := domain.ServiceUnspecified
		if !strings.EqualFold(v, "none") {
			s, err := domain.ParseService(v)
			if err != nil || s == domain.ServiceUnspecified {
				return nil, domain.Invalidf("unknown service %q; use none or one of %s", v, serviceNames())
			}
			svc = s
		}
		return func(ch domain.Channel) bool { return ch.Service == svc }, nil
	case "tag":
		v = strings.ToLower(strings.TrimSpace(v))
		return func(ch domain.Channel) bool { return slices.Contains(ch.AllTags(), v) }, nil
	case "status":
		st, err := domain.ParseStatus(v)
		if err != nil || strings.TrimSpace(v) == "" {
			return nil, domain.Invalidf("unknown status %q; use active, off-air, or retired", v)
		}
		return func(ch domain.Channel) bool { return ch.Status.OrActive() == st }, nil
	case "in":
		return nil, domain.Invalidf("in: chooses which channels to search, so it can't be negated or inside OR or parentheses")
	case "group":
		// Groups became tags named after them, so group:NAME still works
		// in queries saved before.
		if strings.EqualFold(v, "none") {
			return nil, domain.Invalidf("groups are gone; each became a tag, so use -has:tag or -tag:NAME")
		}
		v = strings.ToLower(strings.TrimSpace(v))
		return func(ch domain.Channel) bool { return slices.Contains(ch.AllTags(), v) }, nil
	case "freq", "rx", "tx":
		ok, err := rangeSpec(t.Op, v, domain.ParseFrequencyMHz)
		if err != nil {
			return nil, fmt.Errorf("%w (frequencies are MHz, such as 146.52, 144..148, or >=420)", err)
		}
		return freqMatcher(t.Field, ok), nil
	case "band":
		return bandMatcher(v)
	case "duplex":
		want := strings.ToLower(v)
		if !slices.Contains([]string{"+", "-", "split", "simplex", "off"}, want) {
			return nil, domain.Invalidf("use +, -, split, simplex, or off")
		}
		return func(ch domain.Channel) bool { return domain.Duplex(ch) == want }, nil
	case "tone", "txtone", "rxtone":
		return toneMatcher(t.Field, v)
	case "mode":
		if !slices.ContainsFunc(domain.Modes, func(m string) bool { return strings.EqualFold(m, v) }) {
			return nil, domain.Invalidf("unknown mode %q; use %s", v, strings.Join(domain.Modes, ", "))
		}
		return func(ch domain.Channel) bool { return strings.EqualFold(ch.Mode, v) }, nil
	case "power":
		p := domain.PowerUnset
		if !strings.EqualFold(v, "none") {
			var err error
			if p, err = domain.ParsePowerIntent(v); err != nil || p == domain.PowerUnset {
				return nil, domain.Invalidf("use none, low, medium, high, or max")
			}
		}
		return func(ch domain.Channel) bool { return ch.Power == p }, nil
	case "squelch", "skip":
		want, err := parseBool(v)
		if err != nil {
			return nil, err
		}
		if t.Field == "skip" {
			return func(ch domain.Channel) bool { return ch.Skip == want }, nil
		}
		return func(ch domain.Channel) bool { return ch.ToneSquelch == want }, nil
	case "is":
		return isMatcher(v)
	case "has":
		return c.hasMatcher(v)
	case "near":
		return nearMatcher(v)
	case "radio":
		if c.env.RadioChannels == nil {
			return nil, domain.Invalidf("radios can't be used here")
		}
		ref, invert := v, false
		switch strings.ToLower(v) {
		case "none":
			ref, invert = "", true
		case "any":
			ref = ""
		}
		on, err := c.env.RadioChannels(ref)
		if err != nil {
			return nil, err
		}
		return func(ch domain.Channel) bool { return on[ch.ID] != invert }, nil
	case "tunes":
		if c.env.Tunes == nil {
			return nil, domain.Invalidf("radios can't be used here")
		}
		return c.env.Tunes(v)
	case "created", "updated":
		ok, err := rangeSpec(t.Op, v, parseDate)
		if err != nil {
			return nil, fmt.Errorf("%w (dates are YYYY-MM-DD, such as 2026-10-01 or >=2026-10-01)", err)
		}
		return func(ch domain.Channel) bool {
			at := ch.CreatedAt
			if t.Field == "updated" {
				at = ch.UpdatedAt
			}
			return ok(at.Local().Format(time.DateOnly))
		}, nil
	case "id":
		return func(ch domain.Channel) bool { return ch.ID == v }, nil
	case "org":
		// The operator, by name (ignoring case) or ID.
		return func(ch domain.Channel) bool {
			return slices.ContainsFunc(ch.Orgs, func(r domain.OrgRef) bool {
				return r.Role == domain.RoleOperator && (strings.EqualFold(r.Name, v) || r.OrgID == v)
			})
		}, nil
	case "system":
		if c.env.System == nil {
			return nil, domain.Invalidf("systems can't be used here")
		}
		in, err := c.env.System(v)
		if err != nil {
			return nil, err
		}
		return func(ch domain.Channel) bool {
			return in[ch.ID] || ch.Link != nil && in[ch.Link.SharedID]
		}, nil
	case "allstar", "echolink", "irlp", "wires-x":
		want, err := domain.ParseNetworkID(t.Field + ":" + v)
		if err != nil {
			return nil, err
		}
		return func(ch domain.Channel) bool { return slices.Contains(ch.Networks, want) }, nil
	}
	return nil, domain.Invalidf("unknown field")
}

func serviceNames() string {
	names := make([]string, len(domain.Services))
	for i, s := range domain.Services {
		names[i] = string(s)
	}
	return strings.Join(names, ", ")
}

// textMatcher matches a bare word against names, short names, and notes,
// ignoring case.
func textMatcher(v string) Matcher {
	v = strings.ToLower(v)
	return func(ch domain.Channel) bool {
		fields := append([]string{ch.Name, ch.AllNotes()}, ch.ShortNames...)
		return slices.ContainsFunc(fields, func(s string) bool { return strings.Contains(strings.ToLower(s), v) })
	}
}

// rangeSpec parses an exact value, a comparison (op), or a range a..b with
// either end optional.
func rangeSpec[T cmp.Ordered](op, v string, parse func(string) (T, error)) (func(T) bool, error) {
	one := func(s string) (T, error) {
		x, err := parse(s)
		if err != nil {
			return x, domain.Invalidf("%q is not a valid value", s)
		}
		return x, nil
	}
	if op != "" {
		x, err := one(v)
		if err != nil {
			return nil, err
		}
		switch op {
		case ">":
			return func(y T) bool { return y > x }, nil
		case ">=":
			return func(y T) bool { return y >= x }, nil
		case "<":
			return func(y T) bool { return y < x }, nil
		default:
			return func(y T) bool { return y <= x }, nil
		}
	}
	lo, hi, isRange := strings.Cut(v, "..")
	if !isRange {
		x, err := one(v)
		if err != nil {
			return nil, err
		}
		return func(y T) bool { return y == x }, nil
	}
	if lo == "" && hi == "" {
		return nil, domain.Invalidf("a range needs at least one end")
	}
	var low, high T
	var err error
	if lo != "" {
		if low, err = one(lo); err != nil {
			return nil, err
		}
	}
	if hi != "" {
		if high, err = one(hi); err != nil {
			return nil, err
		}
	}
	if lo != "" && hi != "" && high < low {
		return nil, domain.Invalidf("range %s runs backwards", v)
	}
	return func(y T) bool { return (lo == "" || y >= low) && (hi == "" || y <= high) }, nil
}

func parseDate(s string) (string, error) {
	d, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return "", err
	}
	return d.Format(time.DateOnly), nil
}

func parseBool(v string) (bool, error) {
	switch strings.ToLower(v) {
	case "yes", "true", "on":
		return true, nil
	case "no", "false", "off":
		return false, nil
	}
	return false, domain.Invalidf("use yes or no")
}

// freqMatcher: freq matches either frequency; tx matches only a recorded
// transmit frequency.
func freqMatcher(field string, ok func(int64) bool) Matcher {
	return func(ch domain.Channel) bool {
		tx := ch.TransmitHz != nil && ok(*ch.TransmitHz)
		switch field {
		case "rx":
			return ok(ch.ReceiveHz)
		case "tx":
			return tx
		}
		return ok(ch.ReceiveHz) || tx
	}
}

// IsValues and HasValues are the words is: and has: take.
var (
	IsValues  = []string{"repeater", "simplex", "rxonly", "transmit", "skip"}
	HasValues = []string{"location", "service", "tag", "tone", "power", "notes", "shortname", "radio", "network", "org"}
)

func isMatcher(v string) (Matcher, error) {
	switch strings.ToLower(v) {
	case "repeater":
		return func(ch domain.Channel) bool { return ch.TransmitHz != nil && *ch.TransmitHz != ch.ReceiveHz }, nil
	case "simplex":
		return func(ch domain.Channel) bool { return ch.TransmitHz != nil && *ch.TransmitHz == ch.ReceiveHz }, nil
	case "rxonly":
		return func(ch domain.Channel) bool { return ch.RXOnly || ch.TransmitHz == nil }, nil
	case "transmit":
		// The same rule radio exports apply; see program.Build.
		return func(ch domain.Channel) bool {
			return ch.TransmitHz != nil && !ch.RXOnly && domain.PersonalTransmitAllowed(*ch.TransmitHz)
		}, nil
	case "skip":
		return func(ch domain.Channel) bool { return ch.Skip }, nil
	}
	return nil, domain.Invalidf("unknown value %q; use %s", v, strings.Join(IsValues, ", "))
}

func (c *compiler) hasMatcher(v string) (Matcher, error) {
	switch strings.ToLower(v) {
	case "location":
		return func(ch domain.Channel) bool { return ch.Location != nil }, nil
	case "service":
		return func(ch domain.Channel) bool { return ch.Service != domain.ServiceUnspecified }, nil
	case "group":
		return nil, domain.Invalidf("groups are gone; each became a tag, so use has:tag")
	case "tag", "tags":
		return func(ch domain.Channel) bool { return len(ch.AllTags()) > 0 }, nil
	case "tone", "tones":
		return func(ch domain.Channel) bool {
			tx, rx := domain.ProgrammedTones(ch)
			return tx.Kind != domain.ToneNone || rx.Kind != domain.ToneNone
		}, nil
	case "power":
		return func(ch domain.Channel) bool { return ch.Power != domain.PowerUnset }, nil
	case "notes":
		return func(ch domain.Channel) bool { return strings.TrimSpace(ch.AllNotes()) != "" }, nil
	case "shortname", "shortnames":
		return func(ch domain.Channel) bool { return len(ch.ShortNames) > 0 }, nil
	case "radio":
		return c.term(Term{Field: "radio", Value: "any"})
	case "network", "networks":
		return func(ch domain.Channel) bool { return len(ch.Networks) > 0 }, nil
	case "org", "orgs":
		return func(ch domain.Channel) bool { return len(ch.Orgs) > 0 }, nil
	}
	return nil, domain.Invalidf("unknown value %q; use %s", v, strings.Join(HasValues, ", "))
}

// toneSpec is a tone to look for: a kind alone, or an exact tone.
type toneSpec struct {
	kind  domain.ToneKind
	exact *domain.Tone
}

func parseToneSpec(v string) (toneSpec, error) {
	lv := strings.ToLower(strings.TrimSpace(v))
	switch lv {
	case "none":
		return toneSpec{kind: domain.ToneNone}, nil
	case "ctcss":
		return toneSpec{kind: domain.ToneCTCSS}, nil
	case "dcs":
		return toneSpec{kind: domain.ToneDCS}, nil
	}
	if _, err := strconv.ParseFloat(lv, 64); err == nil {
		lv = "ctcss:" + lv
	}
	t, err := domain.ParseTone(lv)
	if err != nil {
		return toneSpec{}, domain.Invalidf("%q: use a CTCSS frequency such as 131.8, dcs:023, ctcss, dcs, or none", v)
	}
	return toneSpec{kind: t.Kind, exact: &t}, nil
}

func (s toneSpec) matches(t domain.Tone) bool {
	if t.Kind == "" {
		t.Kind = domain.ToneNone
	}
	if s.exact == nil || t.Kind != s.kind {
		return t.Kind == s.kind
	}
	return domain.FormatTone(t) == domain.FormatTone(*s.exact)
}

// toneMatcher looks at tones as programmed (see domain.ProgrammedTones):
// the receive tone only counts when tone squelch is on. tone:none means
// neither direction has a tone; tone:X matches either direction.
func toneMatcher(field, v string) (Matcher, error) {
	spec, err := parseToneSpec(v)
	if err != nil {
		return nil, err
	}
	return func(ch domain.Channel) bool {
		tx, rx := domain.ProgrammedTones(ch)
		switch field {
		case "txtone":
			return spec.matches(tx)
		case "rxtone":
			return spec.matches(rx)
		}
		if spec.kind == domain.ToneNone {
			return spec.matches(tx) && spec.matches(rx)
		}
		return spec.matches(tx) || spec.matches(rx)
	}, nil
}

// nearMatcher parses LAT,LON,KM or GRID,KM, where GRID is a Maidenhead
// grid square searched from its center (KM may end in "km"). A channel is
// near when its location is within KM plus its own radius, so a repeater's
// coverage counts. Channels without a location never match.
func nearMatcher(v string) (Matcher, error) {
	parts := strings.Split(v, ",")
	if len(parts) == 2 && domain.IsGrid(parts[0]) {
		g, err := domain.ParseGrid(parts[0])
		if err != nil {
			return nil, err
		}
		parts = []string{fmt.Sprint(g.Latitude), fmt.Sprint(g.Longitude), parts[1]}
	}
	if len(parts) != 3 {
		return nil, domain.Invalidf("use LAT,LON,KM or GRID,KM, such as 34.64,-120.46,40km or CM94sq,40km")
	}
	var nums [3]float64
	for i, p := range parts {
		p = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(p)), "km")
		n, err := strconv.ParseFloat(p, 64)
		if err != nil {
			return nil, domain.Invalidf("%q is not a number; use LAT,LON,KM or GRID,KM", parts[i])
		}
		nums[i] = n
	}
	lat, lon, km := nums[0], nums[1], nums[2]
	if lat < -90 || lat > 90 || lon < -180 || lon > 180 {
		return nil, domain.Invalidf("latitude must be -90 to 90 and longitude -180 to 180")
	}
	if km <= 0 {
		return nil, domain.Invalidf("the distance must be positive")
	}
	return func(ch domain.Channel) bool {
		l := ch.Location
		if l == nil {
			return false
		}
		reach := km
		if l.RadiusKM != nil {
			reach += *l.RadiusKM
		}
		return domain.DistanceKM(lat, lon, l.Latitude, l.Longitude) <= reach
	}, nil
}
