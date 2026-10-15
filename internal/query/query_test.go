package query

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ryan/rflib/internal/domain"
)

func TestParseCanonical(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"", ""},
		{"tag:mobile", "tag:mobile"},
		{"Tag:Mobile  service:none", "tag:Mobile service:none"},
		{"a b OR c", "a (b OR c)"},
		{"(a b) OR c", "(a b) OR c"},
		{"-tag:hf", "-tag:hf"},
		{"NOT (tag:a OR tag:b)", "-(tag:a OR tag:b)"},
		{"-(a b)", "-(a b)"},
		{`name:"Lompoc PD" "two words"`, `name:"Lompoc PD" "two words"`},
		{"freq:>=440 freq:144..148", "freq:>=440 freq:144..148"},
		{`freq:">=440"`, `freq:">=440"`},
		{"duplex:- duplex:+", "duplex:- duplex:+"},
		{"near:34.6,-120.4,40km", "near:34.6,-120.4,40km"},
		{"@Local OR @wx", "@local OR @wx"},
		{"a AND b", "a b"},
		{"frequency:146.52 tags:x", "freq:146.52 tag:x"},
		{`name:"say \"hi\""`, `name:"say \"hi\""`},
		{`"OR"`, `"OR"`},
		{"x-ray", "x-ray"},
		{"wiresx:123 organization:SBARC", "wires-x:123 org:SBARC"},
	} {
		n, err := Parse(tc.in)
		if err != nil {
			t.Errorf("Parse(%q): %v", tc.in, err)
			continue
		}
		if got := n.String(); got != tc.want {
			t.Errorf("Parse(%q) = %q, want %q", tc.in, got, tc.want)
		}
		// Canonical text parses back to the same tree.
		again, err := Parse(n.String())
		if err != nil || again.String() != n.String() {
			t.Errorf("round trip of %q: %v, %v", tc.in, again, err)
		}
	}
}

func TestPrecedenceORBindsTighter(t *testing.T) {
	n, err := Parse("a b OR c")
	if err != nil {
		t.Fatal(err)
	}
	and, ok := n.(And)
	if !ok || len(and.Nodes) != 2 {
		t.Fatalf("got %#v", n)
	}
	if _, ok := and.Nodes[1].(Or); !ok {
		t.Fatalf("second node = %#v, want Or", and.Nodes[1])
	}
}

func TestParseErrors(t *testing.T) {
	for _, tc := range []struct {
		in, want string
		pos      int
	}{
		{"frq:146", `unknown field "frq"`, 1},
		{"tag:a (b", "missing ) for this (", 7},
		{"tag:a )", `unexpected ")"`, 7},
		{`name:"open`, "missing closing quote", 6},
		{"OR tag:a", "OR needs a term before it", 1},
		{"tag:a OR", "OR needs a term after it", 7},
		{"tag:a -", "", 0}, // a lone - is text
		{"tag:", "tag: needs a value", 1},
		{"()", "empty ( )", 1},
		{"@Bad!", "is not a saved search name", 1},
		{"NOT", "NOT needs a term after it", 1},
	} {
		_, err := Parse(tc.in)
		if tc.want == "" {
			if err != nil {
				t.Errorf("Parse(%q): %v", tc.in, err)
			}
			continue
		}
		var qe *Error
		if !errors.As(err, &qe) || !strings.Contains(qe.Msg, tc.want) || qe.Pos != tc.pos {
			t.Errorf("Parse(%q) = %v, want %q at %d", tc.in, err, tc.want, tc.pos)
		}
	}
}

func hz(v int64) *int64     { return &v }
func fl(v float64) *float64 { return &v }

func ctcss(t *testing.T, v string) domain.Tone {
	t.Helper()
	tone, err := domain.ParseTone(v)
	if err != nil {
		t.Fatal(err)
	}
	return tone
}

// library is a small set of channels covering each field.
func library(t *testing.T) []domain.Channel {
	none := domain.Tone{Kind: domain.ToneNone}
	home := &domain.Location{Latitude: 34.64, Longitude: -120.46, RadiusKM: fl(30)}
	far := &domain.Location{Latitude: 36.0, Longitude: -120.46}
	day := func(s string) time.Time {
		d, _ := time.ParseInLocation(time.DateOnly, s, time.Local)
		return d.Add(12 * time.Hour)
	}
	cs := []domain.Channel{
		{ID: "call", Name: "2m FM Call", ShortNames: []string{"Call"}, ReceiveHz: 146_520_000, TransmitHz: hz(146_520_000), Mode: "FM",
			Service: domain.ServiceAmateur, Tags: []string{"calling", "ham"}, TransmitTone: none, ReceiveTone: none, CreatedAt: day("2026-09-01")},
		{ID: "marian", Name: "Marian", ReceiveHz: 147_300_000, TransmitHz: hz(147_900_000), Mode: "FM", Tags: []string{"ham", "home"},
			TransmitTone: ctcss(t, "ctcss:131.8"), ReceiveTone: none, Location: home, Notes: "Hospital repeater", CreatedAt: day("2026-10-01"),
			Networks: []domain.NetworkID{{Network: domain.NetworkAllStar, Node: "12345"}}, Orgs: []domain.OrgRef{{OrgID: "o1", Name: "SBARC", Role: domain.RoleOperator}}},
		{ID: "uhf", Name: "Tranq UHF", ReceiveHz: 449_140_000, TransmitHz: hz(444_140_000), Mode: "FM", Tags: []string{"far", "ham"},
			TransmitTone: ctcss(t, "ctcss:131.8"), ReceiveTone: ctcss(t, "ctcss:131.8"), ToneSquelch: true, Location: far, CreatedAt: day("2026-10-05"),
			Orgs: []domain.OrgRef{{OrgID: "o2", Name: "GRONK", Role: domain.RolePartner}}, Link: &domain.Link{SharedID: "s-uhf"}},
		{ID: "sheriff", Name: "SBCoSO Ops 1D", ReceiveHz: 460_275_000, TransmitHz: hz(465_275_000), Mode: "NFM",
			TransmitTone: ctcss(t, "dcs:023"), ReceiveTone: ctcss(t, "dcs:023"), ToneSquelch: true, Tags: []string{"statewide"}, CreatedAt: day("2026-10-05")},
		{ID: "wx", Name: "WX1", ReceiveHz: 162_550_000, Mode: "FM", Service: domain.ServiceNOAA, RXOnly: true, Skip: true,
			TransmitTone: none, ReceiveTone: none, Power: domain.PowerLow, CreatedAt: day("2026-10-06")},
		{ID: "gmrs", Name: "GMRS 15R", ReceiveHz: 462_550_000, TransmitHz: hz(467_550_000), Mode: "FM", Service: domain.ServiceGMRS,
			TransmitTone: none, ReceiveTone: ctcss(t, "ctcss:100"), CreatedAt: day("2026-10-06")},
		{ID: "xband", Name: "Cross", ReceiveHz: 146_000_000, TransmitHz: hz(446_000_000), Mode: "FM", TransmitTone: none, ReceiveTone: none},
	}
	for i := range cs {
		if cs[i].UpdatedAt.IsZero() {
			cs[i].UpdatedAt = cs[i].CreatedAt
		}
	}
	return cs
}

var testEnv = Env{
	Search: func(name string) (string, error) {
		switch name {
		case "local":
			return "near:34.64,-120.46,10", nil
		case "ham":
			return "tag:ham -@local", nil
		case "loop":
			return "@loop2", nil
		case "loop2":
			return "tag:x OR @loop", nil
		case "broken":
			return "frq:1", nil
		}
		return "", fmt.Errorf("no saved search named %q", name)
	},
	RadioChannels: func(ref string) (map[string]bool, error) {
		switch strings.ToLower(ref) {
		case "":
			return map[string]bool{"call": true, "wx": true}, nil
		case "ht":
			return map[string]bool{"wx": true}, nil
		}
		return nil, fmt.Errorf("no radio %q", ref)
	},
	Tunes: func(ref string) (func(domain.Channel) bool, error) {
		if ref != "ht" {
			return nil, fmt.Errorf("no radio %q", ref)
		}
		return func(c domain.Channel) bool { return c.ReceiveHz >= 136_000_000 && c.ReceiveHz <= 174_000_000 }, nil
	},
	System: func(ref string) (map[string]bool, error) {
		if !strings.EqualFold(ref, "SBARC Hub") {
			return nil, fmt.Errorf("no system %q", ref)
		}
		return map[string]bool{"marian": true, "s-uhf": true}, nil
	},
}

func TestMatch(t *testing.T) {
	cs := library(t)
	for _, tc := range []struct{ q, want string }{
		{"", "call marian uhf sheriff wx gmrs xband"},
		{"service:none", "marian uhf sheriff xband"},
		{"service:none freq:144..148", "marian xband"},
		{"freq:446", "xband"}, // either frequency
		{"rx:446", ""},
		{"tx:>=440", "uhf sheriff gmrs xband"},
		{"freq:..147", "call xband"},
		{"tag:ham -tag:calling", "marian uhf"},
		{"tag:HAM is:repeater", "marian uhf"},
		{"band:2m OR band:70cm", "call marian uhf xband"},
		{"band:gmrs", "gmrs"},
		{"band:wx", "wx"},
		{"duplex:+", "marian sheriff gmrs"},
		{"duplex:-", "uhf"},
		{"duplex:split", "xband"},
		{"duplex:simplex", "call"},
		{"duplex:off", "wx"},
		{"tone:131.8", "marian uhf"},
		{"tone:dcs", "sheriff"},
		{"tone:dcs:023", "sheriff"},
		{"tone:none", "call wx gmrs xband"}, // GMRS has a receive tone but no squelch
		{"rxtone:131.8", "uhf"},
		{"txtone:ctcss -squelch:yes", "marian"},
		{"has:tone", "marian uhf sheriff"},
		{"mode:nfm", "sheriff"},
		{"mode:cw", ""}, // a known mode, though no channel uses it
		{"power:low", "wx"},
		{"power:none -service:none", "call gmrs"},
		{"skip:yes", "wx"},
		{"is:skip", "wx"},
		{"is:rxonly", "wx"},
		{"is:transmit", "call marian uhf gmrs xband"},
		{"is:simplex", "call"},
		{"near:34.64,-120.46,1km", "marian"},
		{"near:34.64,-120.46,10", "marian"},
		{"near:36.3,-120.46,40", "uhf"},          // ~33 km to Far, which has no radius
		{"near:34.64,-120.46,170", "marian uhf"}, // never the unlocated group
		{"near:34.64,-120.46,10 OR -has:location", "call marian sheriff wx gmrs xband"},
		{"-near:34.64,-120.46,10", "call uhf sheriff wx gmrs xband"},
		{"has:location", "marian uhf"},
		{"group:home", "marian"}, // groups became tags
		{"has:tag -group:far", "call marian sheriff"},
		{"has:service", "call wx gmrs"},
		{"has:notes", "marian"},
		{"has:shortname", "call"},
		{"radio:any", "call wx"},
		{"radio:HT", "wx"},
		{"radio:none", "marian uhf sheriff gmrs xband"},
		{"has:radio", "call wx"},
		{"created:2026-10-05", "uhf sheriff"},
		{"created:>2026-10-01 created:<2026-10-06", "uhf sheriff"},
		{"created:2026-10-01..", "marian uhf sheriff wx gmrs"},
		{"name:call", "call"},
		{"name:SBCo", "sheriff"},
		{"hospital", "marian"},
		{`"fm call"`, "call"},
		{"@local", "marian"},
		{"@ham", "call uhf"},
		{"id:wx", "wx"},
		{"tunes:ht", "call marian wx xband"},
		{"-tunes:ht", "uhf sheriff gmrs"},
		{"tag:ham (is:repeater OR band:70cm) -group:far", "marian"},
		{"org:sbarc", "marian"},
		{"org:o1", "marian"},
		{"org:gronk", ""}, // a partner, not the operator
		{"has:org", "marian uhf"},
		{"has:network", "marian"},
		{"allstar:12345", "marian"},
		{"echolink:12345", ""},
		{`system:"sbarc hub"`, "marian uhf"}, // uhf through its shared channel
	} {
		m, err := CompileText(tc.q, testEnv)
		if err != nil {
			t.Errorf("%q: %v", tc.q, err)
			continue
		}
		var got []string
		for _, c := range cs {
			if m(c) {
				got = append(got, c.ID)
			}
		}
		if g := strings.Join(got, " "); g != tc.want {
			t.Errorf("%q matched %q, want %q", tc.q, g, tc.want)
		}
	}
}

func TestCompileErrors(t *testing.T) {
	for _, tc := range []struct{ q, want string }{
		{"service:ham", `unknown service "ham"`},
		{"tag:>a", "doesn't take >"},
		{"freq:abc", `"abc" is not a valid value`},
		{"freq:148..144", "runs backwards"},
		{"freq:..", "needs at least one end"},
		{"band:4m", `unknown band "4m"`},
		{"duplex:up", "use +, -, split"},
		{"tone:400", "use a CTCSS frequency"},
		{"is:loud", `unknown value "loud"`},
		{"has:wings", `unknown value "wings"`},
		{"near:34,-120", "use LAT,LON,KM"},
		{"near:95,0,10", "latitude must be"},
		{"near:34,-120,0", "must be positive"},
		{"squelch:maybe", "use yes or no"},
		{"created:yesterday", "dates are YYYY-MM-DD"},
		{"group:none", "groups are gone"},
		{"has:group", "groups are gone"},
		{"radio:car", `no radio "car"`},
		{"tunes:car", `no radio "car"`},
		{"mode:dmr", `unknown mode "dmr"`},
		{"power:loud", "use none, low"},
		{"@nope", `no saved search named "nope"`},
		{"@loop", "uses itself (@loop → @loop2 → @loop)"},
		{"@broken", `saved search @broken: query: unknown field "frq"`},
	} {
		_, err := CompileText(tc.q, testEnv)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: error %v, want %q", tc.q, err, tc.want)
		}
	}
	if _, err := CompileText("@local", Env{}); err == nil || !strings.Contains(err.Error(), "can't be used here") {
		t.Errorf("no env: %v", err)
	}
}

func TestRefsAndJoin(t *testing.T) {
	n, err := Parse("@a (tag:x OR @b) -@a")
	if err != nil {
		t.Fatal(err)
	}
	if got := Refs(n); !slices.Equal(got, []string{"a", "b"}) {
		t.Errorf("Refs = %v", got)
	}
	tag, _ := Parse("tag:x OR tag:y")
	if got := Join(n, tag, And{}).String(); got != "@a (tag:x OR @b) -@a (tag:x OR tag:y)" {
		t.Errorf("Join = %q", got)
	}
	if got := Join(And{}, nil).String(); got != "" {
		t.Errorf("empty Join = %q", got)
	}
}
