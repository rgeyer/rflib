// Package program turns a radio's channel list into the memories that get
// programmed: memory numbers, names fitted to the display, power levels,
// and scan settings, with warnings for anything the radio or service rules
// won't accept as asked.
package program

import (
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/ryan/rflib/internal/domain"
	"github.com/ryan/rflib/internal/export"
	"github.com/ryan/rflib/internal/importer"
	"github.com/ryan/rflib/internal/model"
	"github.com/ryan/rflib/internal/naming"
	"github.com/ryan/rflib/internal/profile"
)

// Memory is one memory of a radio's list, as it will be programmed.
type Memory struct {
	// Location is the memory number, or NoNumber for a memory the radio's
	// programming leaves out: it takes no number, so the numbers are the
	// radio's own once it's programmed.
	Location int
	// Channel is the export copy: name fitted, scan and power resolved,
	// and tones as the radio's file holds them.
	Channel domain.Channel
	// Original is the library channel it came from.
	Original domain.Channel
	Name     naming.Result
	// Power is the effective intent before the model mapped it.
	Power domain.PowerIntent
	// LeftOut says why the memory isn't programmed: the channel is retired,
	// or the radio can't use its mode. The memory is listed, with a
	// warning, but not exported, and takes no memory number.
	LeftOut string
	// Invalid is why the radio's file can't hold the memory even with its
	// transmit tone alone; an export fails on it, or leaves it out with
	// skipInvalid. Such a memory takes no number either.
	Invalid error
	// TransmitToneOnly is set when the radio's file can't hold the
	// channel's receive tone (such as split tones), so the memory is
	// programmed with its transmit tone alone.
	TransmitToneOnly bool
	// Overflow is set when the memory is past the end of its section's
	// size: it spills into the memories after the section.
	Overflow bool
}

// NoNumber is the Location of a memory that isn't programmed. Some radios
// number memories from 0.
const NoNumber = -1

// Programmed reports whether an export (leaving out invalid memories)
// programs the memory.
func (m Memory) Programmed() bool { return m.LeftOut == "" && m.Invalid == nil }

// NotProgrammed says why an export (leaving out invalid memories) doesn't
// program the memory; empty when it does.
func (m Memory) NotProgrammed() string {
	if m.LeftOut != "" {
		return m.LeftOut
	}
	if m.Invalid != nil {
		return "the radio's file can't hold it: " + m.Invalid.Error()
	}
	return ""
}

// ReceiveOnly reports whether the memory is programmed receive-only: the
// channel is, or it was made so because it can't transmit there.
func (m Memory) ReceiveOnly() bool { return m.Channel.RXOnly || m.Channel.TransmitHz == nil }

// Layout is a radio's computed memories: what an export writes.
type Layout struct {
	Radio    domain.Radio
	Model    model.Model
	Memories []Memory
	// Sections are the radio's sections, in order, with where each begins.
	Sections []SectionRange
	Warnings []string
}

// SectionRange is a section as laid out: the memory it begins at (even
// when it holds nothing), and the memories its channels use, through Last
// (First - 1 when none).
type SectionRange struct {
	Section domain.RadioSection
	First   int
	Last    int
}

// StartError is a section whose start is behind the memories the sections
// before it already reach.
type StartError struct {
	Section domain.RadioSection
	Reached int
}

func (e *StartError) Error() string {
	return fmt.Sprintf("section %q starts at memory %d, but the sections before it already reach memory %d", e.Section.Name, *e.Section.Start, e.Reached)
}

// Unwrap makes it an invalid-input error, as domain.Invalidf's are.
func (e *StartError) Unwrap() error { return domain.ErrInvalid }

// Build lays out a radio's memories, from its entries in list order (each
// section's together, sections in order). It fails when the list doesn't
// fit the model's memories, or a section's start is behind the sections
// before it; everything else that can't be honored is a warning. Memories
// transmit only where domain.PersonalTransmitAllowed; any other channel is
// programmed receive-only, whatever its service. Memories the programming
// leaves out stay in the list but take no number.
//
// sections are the radio's, in order, empty ones included, since a start or
// a size counts whether or not a section holds anything; without them, the
// sections are those the entries are in.
func Build(r domain.Radio, m model.Model, entries []domain.RadioEntry, sections ...domain.RadioSection) (Layout, error) {
	p := Layout{Radio: r, Model: m}
	dedupe, err := naming.ParseDedupe(orNone(r.Dedupe))
	if err != nil {
		return p, err
	}
	copies := make([]domain.Channel, len(entries))
	notes := make([][]string, len(entries))
	p.Memories = make([]Memory, len(entries))
	for i, e := range entries {
		c := e.Channel
		warn := func(format string, args ...any) { notes[i] = append(notes[i], fmt.Sprintf(format, args...)) }
		if e.Skip != nil {
			c.Skip = *e.Skip
		}
		leftOut := ""
		switch {
		case c.Status.OrActive() == domain.StatusOffAir:
			// Kept, so it needs no reprogramming when it's back, but not
			// scanned while it's silent, unless the radio says to scan it.
			back := ""
			if c.BackOn != nil {
				back = ", expected back " + c.BackOn.Format("2006-01-02")
			}
			if e.Skip != nil && !*e.Skip {
				warn("off the air%s%s; programmed, and scanned as this radio says", noteSuffix(c.StatusNote), back)
			} else {
				c.Skip = true
				warn("off the air%s%s; programmed but skipped in scans", noteSuffix(c.StatusNote), back)
			}
		case c.Status.OrActive() == domain.StatusRetired:
			leftOut = "retired" + noteSuffix(c.StatusNote)
			warn("not programmed: retired%s; see rflib shared changes", noteSuffix(c.StatusNote))
		}
		if leftOut == "" && !m.HasMode(c.Mode) {
			leftOut = fmt.Sprintf("the radio doesn't have %s", c.Mode)
			warn("not programmed: the radio doesn't have %s (it has %s)", c.Mode, strings.Join(m.Modes, ", "))
		}
		if e.Name != "" {
			c.Name, c.ShortNames = e.Name, nil
		}
		intent := domain.ResolvePower(e.Power, c.Power, r.DefaultPower)
		c.ProgrammedPower = choosePower(m, c, intent, warn)
		if c.TransmitHz != nil && !c.RXOnly && !domain.PersonalTransmitAllowed(*c.TransmitHz) {
			c.RXOnly = true
			warn("programmed receive-only: transmit %s MHz is outside the amateur bands and FRS, GMRS, MURS, and CB channels", domain.FormatFrequencyMHz(*c.TransmitHz))
		}
		if !m.Tunes(c.ReceiveHz) {
			warn("%s MHz is outside the radio's bands", domain.FormatFrequencyMHz(c.ReceiveHz))
		}
		if c.TransmitHz != nil && !c.RXOnly && *c.TransmitHz != c.ReceiveHz && !m.Tunes(*c.TransmitHz) {
			warn("transmit %s MHz is outside the radio's bands", domain.FormatFrequencyMHz(*c.TransmitHz))
		}
		copies[i] = c
		p.Memories[i] = Memory{Location: NoNumber, Original: e.Channel, Power: intent, LeftOut: leftOut}
	}
	rules := m.NameRules()
	if r.NameLength > 0 {
		rules.MaxLen = r.NameLength
	}
	named, results := naming.Apply(copies, rules, dedupe)
	for i := range p.Memories {
		p.Memories[i].Channel = named[i]
		p.Memories[i].Name = results[i]
	}
	// What the radio's file can't hold, as the export will find it. When
	// it's the receive tone (such as split tones), the transmit tone wins:
	// it's what opens a repeater.
	ex, err := ExporterFor(p)
	if err != nil {
		return p, err
	}
	for i := range p.Memories {
		mem := &p.Memories[i]
		if mem.LeftOut != "" {
			continue
		}
		if mem.Invalid = ex.Validate(mem.Channel); mem.Invalid == nil || mem.Channel.ReceiveTone.Kind == domain.ToneNone {
			continue
		}
		c := mem.Channel
		c.ReceiveTone, c.ToneSquelch = domain.Tone{Kind: domain.ToneNone}, false
		if ex.Validate(c) == nil {
			notes[i] = append(notes[i], fmt.Sprintf("programmed with the transmit tone only, without receive tone %s: %v", domain.FormatTone(mem.Channel.ReceiveTone), mem.Invalid))
			mem.Channel, mem.Invalid, mem.TransmitToneOnly = c, nil, true
		}
	}
	// Number the memories that are programmed, section by section.
	if sections == nil {
		for i, e := range entries {
			if i == 0 || e.Section.ID != entries[i-1].Section.ID {
				sections = append(sections, e.Section)
			}
		}
	}
	next, i := m.Memories.First, 0
	for _, sec := range sections {
		j := i
		for j < len(entries) && entries[j].Section.ID == sec.ID {
			j++
		}
		if sec.Start != nil {
			if *sec.Start < next {
				return p, &StartError{Section: sec, Reached: next - 1}
			}
			next = *sec.Start
		}
		first := next
		p.Sections = append(p.Sections, SectionRange{Section: sec, First: first, Last: first - 1})
		for k := i; k < j; k++ {
			e := entries[k]
			if !p.Memories[k].Programmed() {
				continue
			}
			if e.MatchPlan && e.PlanNumber != nil {
				// The plan's own number, gaps kept; never backwards.
				next = max(next, first+*e.PlanNumber-1)
			}
			if next > m.Memories.Last {
				return p, domain.Invalidf("%q would be memory %d, but the %s has memories %d-%d", e.Channel.Name, next, m.Description, m.Memories.First, m.Memories.Last)
			}
			p.Memories[k].Location = next
			p.Sections[len(p.Sections)-1].Last = next
			if sec.Size != nil && next >= first+*sec.Size {
				p.Memories[k].Overflow = true
				notes[k] = append(notes[k], fmt.Sprintf("past the end of section %q (%d memories, %d-%d)", sec.Name, *sec.Size, first, first+*sec.Size-1))
			}
			next++
		}
		if sec.Size != nil {
			next = max(next, first+*sec.Size)
		}
		i = j
	}
	if i < len(entries) {
		return p, fmt.Errorf("memory %q is in a section the radio doesn't list", entries[i].Channel.Name)
	}
	for i, mem := range p.Memories {
		where := ""
		if mem.Programmed() {
			where = fmt.Sprintf("memory %d ", mem.Location)
		}
		for _, n := range notes[i] {
			p.Warnings = append(p.Warnings, fmt.Sprintf("%s%q: %s", where, copies[i].Name, n))
		}
	}
	for _, g := range naming.Duplicates(results) {
		p.Warnings = append(p.Warnings, fmt.Sprintf("%d memories are named %q; set the radio's dedupe to index or freq, or give them names", len(g), results[g[0]].Name))
	}
	return p, nil
}

func noteSuffix(note string) string {
	if note == "" {
		return ""
	}
	return " (" + note + ")"
}

func orNone(d string) string {
	if d == "" {
		return string(naming.DedupeNone)
	}
	return d
}

// choosePower maps an intent to one of the model's levels and holds it to
// the channel's service cap. Without an intent the highest legal level is
// chosen under a cap, and high otherwise: a radio with per-memory levels
// needs one on every memory.
func choosePower(m model.Model, c domain.Channel, intent domain.PowerIntent, warn func(string, ...any)) *domain.PowerLevel {
	capWatts, capped := domain.ServiceCapWatts(c)
	if len(m.Power) == 0 {
		if capped {
			warn("%s allows %s W; this radio's power is set from its menu, so check it there", c.Service, fmtWatts(capWatts))
		}
		return nil
	}
	level, ok := m.Level(intent)
	if capped && (!ok || level.Watts == nil || *level.Watts > capWatts) {
		switch legal, found := m.Capped(capWatts); {
		case found:
			if ok && legal.Label != level.Label {
				warn("power lowered from %s to %s for the %s limit of %s W", level.Label, legal.Label, c.Service, fmtWatts(capWatts))
			}
			level, ok = legal, true
		case ok && level.Watts == nil:
			warn("can't check %s against the %s limit of %s W: the model doesn't list its watts", level.Label, c.Service, fmtWatts(capWatts))
		default:
			level, ok = m.Power[0], true
			warn("the lowest level, %s, is above the %s limit of %s W", level.Label, c.Service, fmtWatts(capWatts))
		}
	}
	if !ok {
		level, _ = m.Level(domain.PowerHigh)
	}
	return &domain.PowerLevel{Label: level.Label, Watts: level.Watts}
}

func fmtWatts(w float64) string { return strconv.FormatFloat(w, 'f', -1, 64) }

// Exporter is what writes a layout: CHIRP or the model's profile.
type Exporter interface {
	export.Exporter
	export.Validator
}

// ExporterFor returns the writer for a layout's model, placing each memory at
// its location.
func ExporterFor(p Layout) (Exporter, error) {
	locs := make([]int, len(p.Memories))
	for i, mem := range p.Memories {
		locs[i] = mem.Location
	}
	if name, ok := p.Model.ProfileName(); ok {
		e, err := profile.Find(name)
		if err != nil {
			return nil, err
		}
		return profile.Exporter{Profile: e.Profile, Locations: locs}, nil
	}
	return export.CHIRP{Locations: locs, ToneModes: p.Model.ToneModes, CrossModes: p.Model.CrossModes}, nil
}

// Parse reads a file in a model's format, as its programming software or
// CHIRP saves it, keeping each memory's number.
func Parse(m model.Model, r io.Reader) (importer.Batch, error) {
	if name, ok := m.ProfileName(); ok {
		e, err := profile.Find(name)
		if err != nil {
			return importer.Batch{}, err
		}
		return profile.Parser{Profile: e.Profile}.Parse(r)
	}
	return importer.ParseCHIRP(r)
}

// Write exports a layout. Memories marked LeftOut are not written (Build
// warned about them). With skipInvalid, memories the format can't
// represent (Invalid) are left out too and described on warn; otherwise
// any of them fails the export. Left-out memories' numbers stay empty.
func Write(w io.Writer, p Layout, skipInvalid bool, warn io.Writer) (int, error) {
	p.Memories = slices.DeleteFunc(slices.Clone(p.Memories), func(mem Memory) bool { return mem.LeftOut != "" })
	if skipInvalid {
		var kept []Memory
		for _, mem := range p.Memories {
			if mem.Invalid != nil {
				fmt.Fprintf(warn, "skipped memory %d %q: %v\n", mem.Location, mem.Original.Name, mem.Invalid)
				continue
			}
			kept = append(kept, mem)
		}
		p.Memories = kept
	}
	ex, err := ExporterFor(p)
	if err != nil {
		return 0, err
	}
	cs := make([]domain.Channel, len(p.Memories))
	for i, mem := range p.Memories {
		cs[i] = mem.Channel
	}
	if err := ex.Export(w, cs); err != nil {
		return 0, err
	}
	return len(cs), nil
}
