package model

import (
	"strings"
	"testing"

	"github.com/ryan/rflib/internal/domain"
	"github.com/ryan/rflib/internal/naming"
	"github.com/ryan/rflib/internal/profile"
)

func TestBuiltinsAreValid(t *testing.T) {
	all, err := All()
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, m := range all {
		names = append(names, m.Name)
		if m.Source == "" {
			t.Errorf("%s doesn't cite a source", m.Name)
		}
		if p, ok := m.ProfileName(); ok {
			if _, err := profile.Find(p); err != nil {
				t.Errorf("%s: %v", m.Name, err)
			}
		}
	}
	want := "baofeng-uv5rm,explorer-qrz1,radioddity-uv5g,tidradio-tdh3-gmrs,tyt-th9800,yaesu-ft857,yaesu-ftm150,yaesu-ftm6000"
	if got := strings.Join(names, ","); got != want {
		t.Fatalf("built-ins = %s", got)
	}
}

func mustFind(t *testing.T, name string) Model {
	t.Helper()
	m, err := Find(name)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestLevel(t *testing.T) {
	th := mustFind(t, "tyt-th9800")        // Low, Mid2, Mid1 (medium), High
	uv5g := mustFind(t, "radioddity-uv5g") // Low, High
	ft857 := mustFind(t, "yaesu-ft857")    // no levels
	for _, tc := range []struct {
		m    Model
		p    domain.PowerIntent
		want string
	}{
		{th, domain.PowerLow, "Low"}, {th, domain.PowerMedium, "Mid1"}, {th, domain.PowerHigh, "High"}, {th, domain.PowerMax, "High"},
		{uv5g, domain.PowerMedium, "High"}, // no middle level: err toward being heard
		{uv5g, domain.PowerMax, "High"},
	} {
		l, ok := tc.m.Level(tc.p)
		if !ok || l.Label != tc.want {
			t.Errorf("%s %s = %q, %v; want %s", tc.m.Name, tc.p, l.Label, ok, tc.want)
		}
	}
	if _, ok := ft857.Level(domain.PowerHigh); ok {
		t.Error("FT-857 has no per-memory power")
	}
	if _, ok := th.Level(domain.PowerUnset); ok {
		t.Error("unset intent picked a level")
	}
	if l, ok := uv5g.Capped(2); !ok || l.Label != "Low" {
		t.Errorf("UV-5G capped at 2 W = %+v, %v", l, ok)
	}
	if _, ok := uv5g.Capped(0.5); ok {
		t.Error("UV-5G has no level at 0.5 W")
	}
}

func TestNameRules(t *testing.T) {
	th := mustFind(t, "tyt-th9800").NameRules()
	if got := naming.Fit("Metro rpt #2", nil, th).Name; len(got) > 6 || got != strings.ToUpper(got) {
		t.Errorf("TH-9800 name = %q", got)
	}
	if !th.Upper {
		t.Error("a set without lower case must upper-case names")
	}
	// Literal extras aren't ranges: "+-=" allows those three only.
	td := mustFind(t, "tidradio-tdh3-gmrs").NameRules()
	if !td.Charset.Allows('-') || td.Charset.Allows(',') == false || td.Charset.Allows('~') || td.Charset.Allows('{') {
		t.Error("TD-H3 charset parsed as ranges")
	}
	if td.Upper {
		t.Error("TD-H3 allows lower case")
	}
}

func TestTunes(t *testing.T) {
	q := mustFind(t, "explorer-qrz1")
	if !q.Tunes(146_520_000) || q.Tunes(162_550_000+400_000_000) || q.Tunes(27_185_000) {
		t.Error("QRZ-1 bands")
	}
	if !mustFind(t, "yaesu-ftm150").Tunes(27_000_000) {
		t.Error("models without bands accept anything")
	}
}

func TestValidate(t *testing.T) {
	for _, tc := range []struct{ yaml, want string }{
		{"name: x\nformat: kenwood\nmemories: {first: 1, last: 2}", "format must be"},
		{"name: x\nformat: chirp\nmemories: {first: 5, last: 2}", "memories must run"},
		{"name: x\nformat: chirp\nmemories: {first: 1, last: 2}\npower: [{label: H, watts: 5}, {label: L, watts: 1}]", "lowest to highest"},
		{"name: x\nformat: chirp\nmemories: {first: 1, last: 2}\npower: [{label: A, intent: low}, {label: B, intent: low}]", "more than one level"},
		{"name: x\nformat: chirp\nmemories: {first: 1, last: 2}\nnames: {charset: klingon}", "names.charset"},
	} {
		if _, err := Parse([]byte(tc.yaml)); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("err = %v, want %q", err, tc.want)
		}
	}
}
