package domain

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestFrequencyRoundTrip(t *testing.T) {
	hz, err := ParseFrequencyMHz("146.520")
	if err != nil || hz != 146520000 {
		t.Fatalf("got %d, %v", hz, err)
	}
	if got := FormatFrequencyMHz(hz); got != "146.520000" {
		t.Fatalf("got %q", got)
	}
}
func TestParseTone(t *testing.T) {
	tone, err := ParseTone("ctcss:69.3")
	if err != nil {
		t.Fatal(err)
	}
	if tone.CTCSSMilliHz == nil || *tone.CTCSSMilliHz != 69300 {
		t.Fatalf("CTCSS value = %#v", tone.CTCSSMilliHz)
	}
	if got := FormatTone(tone); got != "ctcss:69.300" {
		t.Fatalf("formatted tone = %q", got)
	}
	for _, tc := range []string{"none", "dcs:023", "dcs:023:inverted"} {
		if _, err := ParseTone(tc); err != nil {
			t.Errorf("ParseTone(%q): %v", tc, err)
		}
	}
	for _, tc := range []string{"ctcss:20", "ctcss:69.3001", "dcs:089", "dcs:000", "dcs:1000"} {
		if _, err := ParseTone(tc); err == nil {
			t.Errorf("invalid tone %q accepted", tc)
		}
	}
}
func TestValidateChannelToneSquelch(t *testing.T) {
	c := Channel{Name: "Test", ReceiveHz: 146520000, Mode: "FM", ReceiveTone: Tone{Kind: ToneNone}, TransmitTone: Tone{Kind: ToneNone}, ToneSquelch: true}
	if err := ValidateChannel(c); err == nil {
		t.Fatal("accepted tone squelch without receive tone")
	}
}

func TestStandardToneLists(t *testing.T) {
	if len(StandardCTCSSMilliHz) != 50 || len(StandardDCSCodes) != 104 {
		t.Fatalf("lists have %d CTCSS and %d DCS entries", len(StandardCTCSSMilliHz), len(StandardDCSCodes))
	}
	for _, tc := range []struct {
		tone string
		want bool
	}{{"none", true}, {"ctcss:100", true}, {"ctcss:100.5", false}, {"dcs:023", true}, {"dcs:024", false}} {
		tone, err := ParseTone(tc.tone)
		if err != nil {
			t.Fatal(err)
		}
		if got := IsStandardTone(tone); got != tc.want {
			t.Errorf("IsStandardTone(%s) = %v", tc.tone, got)
		}
	}
}

func TestRecordChanges(t *testing.T) {
	a := json.RawMessage(`{"name":"Hub","members":[{"id":"a"},{"id":"b"}],"notes":""}`)
	for _, tc := range []struct {
		b    string
		want string
	}{
		{`{"name":"Hub","members":[{"id":"a"},{"id":"b"},{"id":"c"}],"notes":""}`, "members: 2 → 3 (1 added)"},
		{`{"name":"Hub","members":[{"id":"b"},{"id":"a"}],"notes":""}`, "members: 2 → 2 (reordered)"},
		{`{"name":"Hub","members":[{"id":"a"},{"id":"x"}],"notes":"x"}`, "members: 2 → 2 (1 changed); notes: (none) → x"},
		{`{"name":"Hub 2","notes":""}`, "members: 2 → 0 (2 removed); name: Hub → Hub 2"},
	} {
		if got := strings.Join(RecordChanges(a, json.RawMessage(tc.b)), "; "); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.b, got, tc.want)
		}
	}
}
