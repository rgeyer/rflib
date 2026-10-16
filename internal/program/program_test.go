package program

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/ryan/rflib/internal/domain"
	"github.com/ryan/rflib/internal/model"
)

func mustModel(t *testing.T, name string) model.Model {
	t.Helper()
	m, err := model.Find(name)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func ch(name string, rx, tx int64, svc domain.Service) domain.Channel {
	none := domain.Tone{Kind: domain.ToneNone}
	c := domain.Channel{ID: name, Name: name, ReceiveHz: rx, Mode: "FM", Service: svc, ReceiveTone: none, TransmitTone: none}
	if tx != 0 {
		c.TransmitHz = &tx
	}
	return c
}

func entries(cs ...domain.Channel) []domain.RadioEntry {
	out := make([]domain.RadioEntry, len(cs))
	for i, c := range cs {
		out[i] = domain.RadioEntry{Channel: c, Position: i}
	}
	return out
}

func level(m Memory) string {
	if m.Channel.ProgrammedPower == nil {
		return "-"
	}
	return m.Channel.ProgrammedPower.Label
}

func TestPowerPrecedenceAndCaps(t *testing.T) {
	m := mustModel(t, "tyt-th9800")
	own := ch("Own", 146_550_000, 146_550_000, domain.ServiceAmateur)
	own.Power = domain.PowerMax
	plain := ch("Plain", 146_580_000, 146_580_000, domain.ServiceAmateur)
	overridden := ch("Override", 146_490_000, 146_490_000, domain.ServiceAmateur)
	overridden.Power = domain.PowerHigh
	es := entries(own, plain, overridden)
	es[2].Power = domain.PowerLow

	p, err := Build(domain.Radio{Name: "Truck", DefaultPower: domain.PowerMedium}, m, es)
	if err != nil {
		t.Fatal(err)
	}
	got := []string{level(p.Memories[0]), level(p.Memories[1]), level(p.Memories[2])}
	if strings.Join(got, ",") != "High,Mid1,Low" {
		t.Errorf("levels = %v (channel, radio default, radio override)", got)
	}

	// Service caps: GMRS 1-7 allows 5 W; the TH-9800's lowest level is 5 W.
	gmrs1 := ch("GMRS 1", 462_562_500, 462_562_500, domain.ServiceGMRS)
	gmrs1.Power = domain.PowerHigh
	// GMRS 8-14 allows 0.5 W; nothing on this radio is that low.
	gmrs8 := ch("GMRS 8", 467_562_500, 467_562_500, domain.ServiceGMRS)
	p, err = Build(domain.Radio{Name: "Truck"}, m, entries(gmrs1, gmrs8))
	if err != nil {
		t.Fatal(err)
	}
	if level(p.Memories[0]) != "Low" || level(p.Memories[1]) != "Low" {
		t.Errorf("capped levels = %s, %s", level(p.Memories[0]), level(p.Memories[1]))
	}
	warnings := strings.Join(p.Warnings, "\n")
	for _, want := range []string{"lowered from High to Low for the gmrs limit of 5 W", "lowest level, Low, is above the gmrs limit of 0.5 W"} {
		if !strings.Contains(warnings, want) {
			t.Errorf("missing warning %q in:\n%s", want, warnings)
		}
	}

	// With no intent anywhere, a radio with levels still gets one on every
	// memory, receive-only or not: CHIRP rejects a blank power.
	sheriff := ch("Sheriff", 460_275_000, 460_275_000, "")
	p, err = Build(domain.Radio{Name: "HT"}, mustModel(t, "baofeng-uv5rm"), entries(plain, sheriff))
	if err != nil {
		t.Fatal(err)
	}
	if level(p.Memories[0]) != "High" || level(p.Memories[1]) != "High" {
		t.Errorf("unset levels = %s, %s; want High", level(p.Memories[0]), level(p.Memories[1]))
	}
	var out bytes.Buffer
	if _, err := Write(&out, p, false, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	for _, row := range strings.Split(strings.TrimSpace(out.String()), "\r\n")[1:] {
		if power := strings.Split(row, ",")[15]; power != "8W" {
			t.Errorf("CHIRP power %q in %s", power, row)
		}
	}

	// A radio without per-memory power can only be warned about.
	p, _ = Build(domain.Radio{Name: "Shack"}, mustModel(t, "yaesu-ft857"), entries(gmrs1))
	if level(p.Memories[0]) != "-" || !strings.Contains(strings.Join(p.Warnings, ""), "set from its menu") {
		t.Errorf("FT-857: %s, %v", level(p.Memories[0]), p.Warnings)
	}
}

func TestNumberingNamesAndScan(t *testing.T) {
	m := mustModel(t, "radioddity-uv5g") // memories 0-127, 7 upper-case characters
	cs := []domain.Channel{
		ch("Metro Repeater", 146_940_000, 146_340_000, domain.ServiceAmateur),
		ch("Calling", 146_520_000, 146_520_000, domain.ServiceAmateur),
		ch("CB 19", 27_185_000, 0, domain.ServiceCB),
	}
	es := entries(cs...)
	ten, skip, scan := 10, true, false
	es[1].SectionStart = &ten
	es[1].Skip = &skip
	cs[2].Skip = true
	es[2].Channel.Skip = true
	es[2].Skip = &scan
	es[2].Name = "Trucker"

	p, err := Build(domain.Radio{Name: "HT"}, m, es)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, mem := range p.Memories {
		got = append(got, strings.Join([]string{strconv.Itoa(mem.Location), mem.Channel.Name, strconv.FormatBool(mem.Channel.Skip)}, ":"))
	}
	if strings.Join(got, " ") != "0:MTRRPT:false 10:CALLING:true 11:TRUCKER:false" {
		t.Errorf("memories = %v", got)
	}
	if !strings.Contains(strings.Join(p.Warnings, ""), "27.185000 MHz is outside the radio's bands") {
		t.Errorf("expected a band warning: %v", p.Warnings)
	}

	// Section starts must not run backwards, and the list must fit.
	five := 5
	es[2].SectionStart = &five
	if _, err := Build(domain.Radio{Name: "HT"}, m, es); err == nil || !strings.Contains(err.Error(), "already reach memory 10") {
		t.Errorf("backwards section: %v", err)
	}
	es[2].SectionStart = nil
	big := 127
	es[1].SectionStart = &big
	if _, err := Build(domain.Radio{Name: "HT"}, m, es); err == nil || !strings.Contains(err.Error(), "memory 128") {
		t.Errorf("overflow: %v", err)
	}
}

func TestWriteCHIRPAndProfile(t *testing.T) {
	gmrs := ch("GMRS 15R", 462_550_000, 467_550_000, domain.ServiceGMRS)
	gmrs.Power = domain.PowerHigh
	ham := ch("Metro", 146_940_000, 146_340_000, domain.ServiceAmateur)
	es := entries(gmrs, ham)
	twenty := 20
	es[1].SectionStart = &twenty

	p, err := Build(domain.Radio{Name: "HT", DefaultPower: domain.PowerLow}, mustModel(t, "tidradio-tdh3-gmrs"), es)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if _, err := Write(&out, p, false, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\r\n")
	if len(lines) != 3 || !strings.HasPrefix(lines[1], "1,GMRS 15R,") || !strings.Contains(lines[1], ",5W,") ||
		!strings.HasPrefix(lines[2], "20,Metro,") || !strings.Contains(lines[2], ",2W,") {
		t.Fatalf("CHIRP export:\n%s", out.String())
	}

	p, err = Build(domain.Radio{Name: "Truck"}, mustModel(t, "yaesu-ftm6000"), es)
	if err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if _, err := Write(&out, p, false, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	rows := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(rows) != 999 || !strings.HasPrefix(rows[0], "1,462.55000,467.55000,") || !strings.Contains(rows[0], ",HIGH,") ||
		rows[1] != "2,,,,,,,,,,,,,,,,,,0" || !strings.HasPrefix(rows[19], "20,146.94000,") {
		t.Fatalf("profile export rows 1, 2, 20:\n%s\n%s\n%s", rows[0], rows[1], rows[19])
	}
}

// Channels transmit only where a ham or personal-radio user may, whatever
// their service says; everything else is programmed receive-only.
func TestTransmitOnlyWhereAllowed(t *testing.T) {
	m := mustModel(t, "baofeng-uv5rm")
	sheriff := ch("SBCoSO Ops 1D", 460_275_000, 465_275_000, domain.ServiceUnspecified)
	labelled := ch("Fire", 154_430_000, 158_955_000, domain.ServiceAmateur) // mislabelled
	repeater := ch("Marian", 147_300_000, 147_900_000, domain.ServiceUnspecified)
	gmrs := ch("GMRS 15R", 462_550_000, 467_550_000, domain.ServiceGMRS)
	p, err := Build(domain.Radio{Name: "HT"}, m, entries(sheriff, labelled, repeater, gmrs))
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []bool{true, true, false, false} {
		if got := p.Memories[i].Channel.RXOnly; got != want {
			t.Errorf("memory %d rx-only = %v, want %v", i, got, want)
		}
	}
	if w := strings.Join(p.Warnings, "\n"); strings.Count(w, "programmed receive-only") != 2 {
		t.Errorf("warnings = %q", w)
	}
	var out bytes.Buffer
	if _, err := Write(&out, p, false, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "1,SBCoSOOps1D,460.275000,off,") {
		t.Errorf("sheriff not transmit-inhibited in CHIRP:\n%s", out.String())
	}
}

// A radio's name length overrides its model's, so chirp-generic radios get
// readable names.
func TestRadioNameLengthOverridesModel(t *testing.T) {
	c := ch("Santa Barbara Sheriff Dispatch", 460_275_000, 0, domain.ServiceUnspecified)
	for _, tc := range []struct {
		length int
		want   int
	}{{0, len(c.Name)}, {8, 8}} {
		p, err := Build(domain.Radio{Name: "HT", NameLength: tc.length}, mustModel(t, "chirp-generic"), entries(c))
		if err != nil {
			t.Fatal(err)
		}
		if got := len([]rune(p.Memories[0].Name.Name)); got > tc.want || (tc.length == 0 && got != tc.want) {
			t.Errorf("name length %d: name %q", tc.length, p.Memories[0].Name.Name)
		}
	}
}

// CW is left out on an FM-only mobile and kept on the all-mode FT-857.
func TestCWNeedsARadioWithCW(t *testing.T) {
	cw := ch("NCDXF 20m", 14_100_000, 0, domain.ServiceAmateur)
	cw.RXOnly, cw.Mode = true, "CW"
	for name, leftOut := range map[string]bool{"yaesu-ftm150": true, "yaesu-ft857": false} {
		p, err := Build(domain.Radio{Name: name}, mustModel(t, name), entries(cw))
		if err != nil {
			t.Fatal(err)
		}
		if (p.Memories[0].LeftOut != "") != leftOut {
			t.Errorf("%s: left out = %v, want %v (warnings %q)", name, p.Memories[0].LeftOut, leftOut, p.Warnings)
		}
	}
}

// A memory in a mode the radio lacks isn't exported and takes no number:
// the next one takes its place.
func TestLeavesOutModesTheRadioLacks(t *testing.T) {
	ssb := ch("2m SSB Call", 144_200_000, 144_200_000, domain.ServiceAmateur)
	ssb.Mode = "USB"
	fm := ch("2m FM Call", 146_520_000, 146_520_000, domain.ServiceAmateur)
	p, err := Build(domain.Radio{Name: "HT"}, mustModel(t, "baofeng-uv5rm"), entries(ssb, fm))
	if err != nil {
		t.Fatal(err)
	}
	if p.Memories[0].Programmed() || !p.Memories[1].Programmed() || !strings.Contains(strings.Join(p.Warnings, "\n"), `"2m SSB Call": not programmed: the radio doesn't have USB`) {
		t.Fatalf("layout = %+v", p)
	}
	var out bytes.Buffer
	n, err := Write(&out, p, false, &bytes.Buffer{})
	lines := strings.Split(strings.TrimSpace(out.String()), "\r\n")
	if err != nil || n != 1 || len(lines) != 2 || !strings.HasPrefix(lines[1], "1,2m FM Call,") || p.Memories[0].Location != NoNumber || p.Memories[1].Location != 1 {
		t.Fatalf("export (%d, %v):\n%s", n, err, out.String())
	}
	if p.Memories[0].Programmed() {
		t.Error("Write changed the layout")
	}
}

// Off-air channels stay programmed but aren't scanned; retired ones aren't
// programmed at all, their numbers kept empty, and come back if they're
// un-retired.
func TestStatusOnTheRadio(t *testing.T) {
	down := ch("Marian", 147_300_000, 147_900_000, domain.ServiceAmateur)
	down.Status, down.StatusNote = domain.StatusOffAir, "antenna work"
	gone := ch("Old Rpt", 146_940_000, 146_340_000, domain.ServiceAmateur)
	gone.Status = domain.StatusRetired
	es := entries(down, gone)
	scan := false
	es[0].Skip = &scan // even a radio set to scan it skips it while it's down
	p, err := Build(domain.Radio{Name: "HT"}, mustModel(t, "baofeng-uv5rm"), es)
	if err != nil {
		t.Fatal(err)
	}
	warnings := strings.Join(p.Warnings, "\n")
	if !p.Memories[0].Channel.Skip || !p.Memories[0].Programmed() || p.Memories[1].NotProgrammed() != "retired" ||
		!strings.Contains(warnings, `"Marian": off the air (antenna work); programmed but skipped in scans`) || !strings.Contains(warnings, `"Old Rpt": not programmed: retired`) {
		t.Errorf("memories = %+v\n%s", p.Memories, warnings)
	}
	var out bytes.Buffer
	if n, err := Write(&out, p, false, &bytes.Buffer{}); err != nil || n != 1 || strings.Contains(out.String(), "Old Rpt") {
		t.Errorf("export (%d, %v):\n%s", n, err, out.String())
	}
	gone.Status = domain.StatusActive
	if p, err = Build(domain.Radio{Name: "HT"}, mustModel(t, "baofeng-uv5rm"), entries(down, gone)); err != nil || !p.Memories[1].Programmed() {
		t.Errorf("un-retired: %+v, %v", p.Memories[1], err)
	}
}

// A radio that can't hold split tones programs the transmit tone alone,
// and a receive-only memory says so.
func TestMemoriesSayWhatIsProgrammed(t *testing.T) {
	split := ch("Split", 145_180_000, 144_580_000, domain.ServiceAmateur)
	split.TransmitTone, split.ReceiveTone, split.ToneSquelch = tone(t, "ctcss:131.8"), tone(t, "ctcss:146.2"), true
	wx := ch("WX1", 162_550_000, 0, domain.ServiceNOAA)
	p, err := Build(domain.Radio{Name: "Shack"}, mustModel(t, "yaesu-ftm6000"), entries(split, wx))
	if err != nil {
		t.Fatal(err)
	}
	if mem := p.Memories[0]; !mem.Programmed() || !mem.TransmitToneOnly || mem.Channel.ReceiveTone.Kind != domain.ToneNone || mem.Channel.ToneSquelch ||
		domain.FormatTone(mem.Channel.TransmitTone) != "ctcss:131.800" || !strings.Contains(strings.Join(p.Warnings, "\n"), "programmed with the transmit tone only") {
		t.Errorf("split tones: %+v\n%s", mem, p.Warnings)
	}
	if !p.Memories[1].Programmed() || !p.Memories[1].ReceiveOnly() || p.Memories[0].ReceiveOnly() {
		t.Errorf("receive only: %+v", p.Memories)
	}
}

func tone(t *testing.T, v string) domain.Tone {
	t.Helper()
	tn, err := domain.ParseTone(v)
	if err != nil {
		t.Fatal(err)
	}
	return tn
}

// A memory that isn't programmed takes no number, so the numbers are the
// radio's own; a section start on it moves to the next one programmed.
func TestUnprogrammedMemoriesTakeNoNumber(t *testing.T) {
	a := ch("A", 146_520_000, 146_520_000, domain.ServiceAmateur)
	gone := ch("Gone", 146_940_000, 146_340_000, domain.ServiceAmateur)
	gone.Status = domain.StatusRetired
	b := ch("B", 146_550_000, 146_550_000, domain.ServiceAmateur)
	c := ch("C", 146_580_000, 146_580_000, domain.ServiceAmateur)
	es := entries(a, gone, b, gone, c)
	es[3].Channel.ID = "Gone2"
	ten := 10
	es[3].SectionStart = &ten
	p, err := Build(domain.Radio{Name: "HT"}, mustModel(t, "baofeng-uv5rm"), es)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, m := range p.Memories {
		got = append(got, fmt.Sprintf("%s=%d", m.Original.Name, m.Location))
	}
	if strings.Join(got, " ") != "A=1 Gone=-1 B=2 Gone=-1 C=10" {
		t.Errorf("numbers = %s", strings.Join(got, " "))
	}
}
