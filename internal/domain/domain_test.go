package domain

import "testing"

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
	for _, tc := range []string{"ctcss:20", "ctcss:69.3001"} {
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
