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
	"github.com/ryan/rflib/internal/model"
	"github.com/ryan/rflib/internal/naming"
	"github.com/ryan/rflib/internal/profile"
)

// Memory is one programmed memory.
type Memory struct {
	Location int
	// Channel is the export copy: name fitted, scan and power resolved.
	Channel domain.Channel
	// Original is the library channel it came from.
	Original domain.Channel
	Name     naming.Result
	// Power is the effective intent before the model mapped it.
	Power domain.PowerIntent
	// LeftOut is set when the radio can't use the channel's mode; the
	// memory is listed, with a warning, but not exported.
	LeftOut bool
}

type Plan struct {
	Radio    domain.Radio
	Model    model.Model
	Memories []Memory
	Warnings []string
}

// Build plans a radio's memories. It fails when the list doesn't fit the
// model's memories; everything else that can't be honored is a warning.
// Memories transmit only where domain.PersonalTransmitAllowed; any other
// channel is programmed receive-only, whatever its service.
func Build(r domain.Radio, m model.Model, entries []domain.RadioEntry) (Plan, error) {
	p := Plan{Radio: r, Model: m}
	locs, err := domain.MemoryNumbers(entries, m.Memories.First)
	if err != nil {
		return p, err
	}
	if n := len(locs); n > 0 && locs[n-1] > m.Memories.Last {
		return p, fmt.Errorf("%q would be memory %d, but the %s has memories %d-%d", entries[n-1].Channel.Name, locs[n-1], m.Description, m.Memories.First, m.Memories.Last)
	}
	dedupe, err := naming.ParseDedupe(orNone(r.Dedupe))
	if err != nil {
		return p, err
	}
	copies := make([]domain.Channel, len(entries))
	p.Memories = make([]Memory, len(entries))
	for i, e := range entries {
		c := e.Channel
		warn := func(format string, args ...any) {
			p.Warnings = append(p.Warnings, fmt.Sprintf("memory %d %q: %s", locs[i], c.Name, fmt.Sprintf(format, args...)))
		}
		if e.Skip != nil {
			c.Skip = *e.Skip
		}
		switch c.Status.OrActive() {
		case domain.StatusOffAir:
			// Kept, so it needs no reprogramming when it's back, but not
			// scanned while it's silent.
			c.Skip = true
			back := ""
			if c.BackOn != nil {
				back = ", expected back " + c.BackOn.Format("2006-01-02")
			}
			warn("off the air%s%s; programmed but skipped in scans", noteSuffix(c.StatusNote), back)
		case domain.StatusRetired:
			warn("retired%s; see rflib shared changes", noteSuffix(c.StatusNote))
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
		leftOut := !m.HasMode(c.Mode)
		if leftOut {
			warn("left out: the radio doesn't have %s (it has %s)", c.Mode, strings.Join(m.Modes, ", "))
		}
		if !m.Tunes(c.ReceiveHz) {
			warn("%s MHz is outside the radio's bands", domain.FormatFrequencyMHz(c.ReceiveHz))
		}
		if c.TransmitHz != nil && !c.RXOnly && *c.TransmitHz != c.ReceiveHz && !m.Tunes(*c.TransmitHz) {
			warn("transmit %s MHz is outside the radio's bands", domain.FormatFrequencyMHz(*c.TransmitHz))
		}
		copies[i] = c
		p.Memories[i] = Memory{Location: locs[i], Original: e.Channel, Power: intent, LeftOut: leftOut}
	}
	named, results := naming.Apply(copies, m.NameRules(), dedupe)
	for i := range p.Memories {
		p.Memories[i].Channel = named[i]
		p.Memories[i].Name = results[i]
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

// Exporter is what writes a plan: CHIRP or the model's profile.
type Exporter interface {
	export.Exporter
	export.Validator
}

// ExporterFor returns the writer for a plan's model, placing each memory at
// its location.
func ExporterFor(p Plan) (Exporter, error) {
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
	return export.CHIRP{Locations: locs}, nil
}

// Write exports a plan. Memories marked LeftOut are not written (Build
// warned about them). With skipInvalid, memories the format can't
// represent are left out too and described on warn; otherwise any of them
// fails the export. Left-out memories' numbers stay empty.
func Write(w io.Writer, p Plan, skipInvalid bool, warn io.Writer) (int, error) {
	p.Memories = slices.DeleteFunc(slices.Clone(p.Memories), func(mem Memory) bool { return mem.LeftOut })
	if skipInvalid {
		var kept []Memory
		ex, err := ExporterFor(p)
		if err != nil {
			return 0, err
		}
		for _, mem := range p.Memories {
			if err := ex.Validate(mem.Channel); err != nil {
				fmt.Fprintf(warn, "skipped memory %d %q: %v\n", mem.Location, mem.Original.Name, err)
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
