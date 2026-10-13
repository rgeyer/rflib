package chirp

import (
	"strings"
	"testing"

	"github.com/ryan/rflib/internal/domain"
)

func tone(t *testing.T, v string) domain.Tone {
	t.Helper()
	x, err := domain.ParseTone(v)
	if err != nil {
		t.Fatal(err)
	}
	return x
}

func hz(mhz string) *int64 {
	v, err := domain.ParseFrequencyMHz(mhz)
	if err != nil {
		panic(err)
	}
	return &v
}

// Each case lists the columns that matter, in Columns order from Duplex to
// CrossMode: Duplex, Offset, Tone, rToneFreq, cToneFreq, DtcsCode,
// DtcsPolarity, RxDtcsCode, CrossMode.
func TestFromChannelToneAndDuplex(t *testing.T) {
	for _, tc := range []struct {
		name        string
		tx          *int64
		txTone      string
		rxTone      string
		squelch     bool
		want        string
		lossyRxTone bool // rx tone without squelch isn't programmable
	}{
		{"simplex no tone", hz("146.52"), "none", "none", false, ",0.000000,,88.5,88.5,023,NN,023,Tone->Tone", false},
		{"receive only", nil, "none", "none", false, "off,0.000000,,88.5,88.5,023,NN,023,Tone->Tone", false},
		{"minus offset tone", hz("145.92"), "ctcss:100", "none", false, "-,0.600000,Tone,100.0,88.5,023,NN,023,Tone->Tone", false},
		{"rx tone without squelch", hz("145.92"), "ctcss:100", "ctcss:100", false, "-,0.600000,Tone,100.0,88.5,023,NN,023,Tone->Tone", true},
		{"plus offset tsql", hz("147.12"), "ctcss:69.3", "ctcss:69.3", true, "+,0.600000,TSQL,69.3,69.3,023,NN,023,Tone->Tone", false},
		{"dtcs inverted rx", hz("146.52"), "dcs:023", "dcs:023:inverted", true, ",0.000000,DTCS,88.5,88.5,023,NR,023,Tone->Tone", false},
		{"split cross-band", hz("446.00"), "none", "none", false, "split,446.000000,,88.5,88.5,023,NN,023,Tone->Tone", false},
		{"cross tone->tone", hz("146.52"), "ctcss:100", "ctcss:123", true, ",0.000000,Cross,100.0,123.0,023,NN,023,Tone->Tone", false},
		{"cross tone->dtcs", hz("146.52"), "ctcss:100", "dcs:754", true, ",0.000000,Cross,100.0,88.5,023,NN,754,Tone->DTCS", false},
		{"cross dtcs->tone", hz("146.52"), "dcs:065:inverted", "ctcss:100", true, ",0.000000,Cross,88.5,100.0,065,RN,023,DTCS->Tone", false},
		{"cross dtcs->dtcs", hz("146.52"), "dcs:023", "dcs:754", true, ",0.000000,Cross,88.5,88.5,023,NN,754,DTCS->DTCS", false},
		{"cross ->tone", hz("146.52"), "none", "ctcss:100", true, ",0.000000,Cross,88.5,100.0,023,NN,023,->Tone", false},
		{"cross ->dtcs", hz("146.52"), "none", "dcs:023", true, ",0.000000,Cross,88.5,88.5,023,NN,023,->DTCS", false},
		{"cross dtcs->", hz("146.52"), "dcs:023", "none", false, ",0.000000,Cross,88.5,88.5,023,NN,023,DTCS->", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := domain.Channel{Name: "Test", ReceiveHz: 146520000, TransmitHz: tc.tx, Mode: "FM", TransmitTone: tone(t, tc.txTone), ReceiveTone: tone(t, tc.rxTone), ToneSquelch: tc.squelch}
			m, err := FromChannel(c, 1, 0)
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.Join(m.Record()[3:12], ","); got != tc.want {
				t.Fatalf("got  %s\nwant %s", got, tc.want)
			}

			back, err := ToChannel(m)
			if err != nil {
				t.Fatal(err)
			}
			if tc.lossyRxTone {
				c.ReceiveTone = domain.Tone{Kind: domain.ToneNone}
			}
			if domain.FormatTone(back.TransmitTone) != domain.FormatTone(c.TransmitTone) || domain.FormatTone(back.ReceiveTone) != domain.FormatTone(c.ReceiveTone) || back.ToneSquelch != c.ToneSquelch {
				t.Errorf("round trip tones: got tx=%s rx=%s sq=%v", domain.FormatTone(back.TransmitTone), domain.FormatTone(back.ReceiveTone), back.ToneSquelch)
			}
			if (back.TransmitHz == nil) != (c.TransmitHz == nil) || back.TransmitHz != nil && *back.TransmitHz != *c.TransmitHz {
				t.Errorf("round trip tx = %v, want %v", back.TransmitHz, c.TransmitHz)
			}
		})
	}
}

func TestFromChannelRejectsWhatCHIRPCannotProgram(t *testing.T) {
	base := domain.Channel{Name: "x", ReceiveHz: 146520000, Mode: "FM", ReceiveTone: tone(t, "none"), TransmitTone: tone(t, "none")}

	odd := base
	odd.TransmitTone = tone(t, "ctcss:100.5")
	if _, err := FromChannel(odd, 0, 0); err == nil || !strings.Contains(err.Error(), "standard") {
		t.Errorf("non-standard CTCSS: err = %v", err)
	}

	// A non-standard receive tone only matters when it's used for squelch.
	unused := base
	unused.ReceiveTone = tone(t, "dcs:024")
	if _, err := FromChannel(unused, 0, 0); err != nil {
		t.Errorf("unused rx tone rejected: %v", err)
	}
	unused.ToneSquelch = true
	if _, err := FromChannel(unused, 0, 0); err == nil {
		t.Error("non-standard squelch DCS accepted")
	}
}

func TestFromChannelFormatting(t *testing.T) {
	c := domain.Channel{Name: "Portland Metro Repeater", ReceiveHz: 146940000, TransmitHz: hz("146.34"), Mode: "NFM", ReceiveTone: tone(t, "none"), TransmitTone: tone(t, "none"), Skip: true, ProgrammedPower: &domain.PowerLevel{Label: "High", Watts: &[]float64{5}[0]}, Notes: "line one\nline two"}
	m, err := FromChannel(c, 7, 8)
	if err != nil {
		t.Fatal(err)
	}
	got := m.Record()
	if len(got) != len(Columns) {
		t.Fatalf("record has %d fields, header %d", len(got), len(Columns))
	}
	want := map[string]string{"Location": "7", "Name": "Portland", "Frequency": "146.940000", "Mode": "NFM", "TStep": "5.00", "Skip": "S", "Power": "5W", "Comment": "line one line two"}
	for i, col := range Columns {
		if w, ok := want[col]; ok && got[i] != w {
			t.Errorf("%s = %q, want %q", col, got[i], w)
		}
	}
}

func TestToChannelErrors(t *testing.T) {
	for _, m := range []Memory{
		{FrequencyHz: 146520000, Mode: "DV"},
		{FrequencyHz: 146520000, Mode: "FM", Duplex: "?"},
		{FrequencyHz: 146520000, Mode: "FM", ToneMode: "TSQL-R"},
		{FrequencyHz: 146520000, Mode: "FM", ToneMode: "Cross", CrossMode: "Tone->Bogus"},
		{FrequencyHz: 1000000, Mode: "FM", Duplex: "-", OffsetHz: 5000000},
	} {
		if _, err := ToChannel(m); err == nil {
			t.Errorf("ToChannel(%+v) accepted", m)
		}
	}
	c, err := ToChannel(Memory{FrequencyHz: 162550000, Mode: "FM", Duplex: "off"})
	if err != nil || c.Name != "162.550000" {
		t.Fatalf("unnamed memory: %+v, %v", c, err)
	}
}

func TestFromChannelRXOnlyKeepsTXOffTheAir(t *testing.T) {
	c := domain.Channel{Name: "Dispatch", ReceiveHz: 460275000, TransmitHz: hz("465.275"), RXOnly: true, Mode: "FM", TransmitTone: tone(t, "ctcss:110.9"), ReceiveTone: tone(t, "ctcss:110.9"), ToneSquelch: true}
	m, err := FromChannel(c, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if m.Duplex != "off" || m.ToneMode != "TSQL" {
		t.Fatalf("duplex = %q, tone = %q", m.Duplex, m.ToneMode)
	}
}

func TestChirpPowerIsWatts(t *testing.T) {
	half := 0.5
	for _, tc := range []struct {
		p    *domain.PowerLevel
		want string
	}{
		{nil, ""},
		{&domain.PowerLevel{Label: "High"}, ""}, // watts unknown: CHIRP's default
		{&domain.PowerLevel{Label: "Low", Watts: &half}, "0.5W"},
	} {
		if got := chirpPower(tc.p); got != tc.want {
			t.Errorf("chirpPower(%+v) = %q, want %q", tc.p, got, tc.want)
		}
	}
}
