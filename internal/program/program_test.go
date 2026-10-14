package program

import (
	"bytes"
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
	group := &domain.Group{Name: "Local", Power: domain.PowerLow}
	inGroup := ch("Grouped", 146_520_000, 146_520_000, domain.ServiceAmateur)
	inGroup.Group = group
	own := ch("Own", 146_550_000, 146_550_000, domain.ServiceAmateur)
	own.Group, own.Power = group, domain.PowerMax
	plain := ch("Plain", 146_580_000, 146_580_000, domain.ServiceAmateur)
	overridden := ch("Override", 146_490_000, 146_490_000, domain.ServiceAmateur)
	overridden.Power = domain.PowerHigh
	es := entries(inGroup, own, plain, overridden)
	es[3].Power = domain.PowerLow

	p, err := Build(domain.Radio{Name: "Truck", DefaultPower: domain.PowerMedium}, m, es)
	if err != nil {
		t.Fatal(err)
	}
	got := []string{level(p.Memories[0]), level(p.Memories[1]), level(p.Memories[2]), level(p.Memories[3])}
	if strings.Join(got, ",") != "Low,High,Mid1,Low" {
		t.Errorf("levels = %v (group, channel over group, radio default, radio override)", got)
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
