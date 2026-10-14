package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/ryan/rflib/internal/domain"
	"github.com/ryan/rflib/internal/program"
)

// Field names match query fields where they overlap, so a value can be
// copied into a search.

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func mhz(hz *int64) string {
	if hz == nil {
		return ""
	}
	return domain.FormatFrequencyMHz(*hz)
}

// offsetMHz is a repeater offset's size, for + and - channels.
func offsetMHz(c domain.Channel) string {
	switch domain.Duplex(c) {
	case "+", "-":
		d := *c.TransmitHz - c.ReceiveHz
		return domain.FormatFrequencyMHz(max(d, -d))
	}
	return ""
}

// toneValue is one tone as a query writes it: 131.8 or dcs:023.
func toneValue(t domain.Tone) string {
	switch {
	case t.Kind == domain.ToneCTCSS && t.CTCSSMilliHz != nil:
		return fmt.Sprintf("%.1f", float64(*t.CTCSSMilliHz)/1000)
	case t.Kind == domain.ToneDCS && t.DCSCode != nil:
		if t.Inverted {
			return fmt.Sprintf("dcs:%03d:inverted", *t.DCSCode)
		}
		return fmt.Sprintf("dcs:%03d", *t.DCSCode)
	}
	return ""
}

// toneSummary is a channel's tones as programmed, as repeater lists write
// them: T131.8 (encode), TSQL 131.8 (encode and squelch on the same tone),
// or T131.8 R88.5. DCS codes read D023N (normal) or D023I (inverted).
func toneSummary(c domain.Channel) string {
	short := func(t domain.Tone) string {
		if t.Kind == domain.ToneDCS && t.DCSCode != nil {
			return fmt.Sprintf("D%03d%s", *t.DCSCode, map[bool]string{false: "N", true: "I"}[t.Inverted])
		}
		return toneValue(t)
	}
	tx, rx := domain.ProgrammedTones(c)
	switch t, r := short(tx), short(rx); {
	case t == "" && r == "":
		return ""
	case r == "":
		return "T" + t
	case t == "":
		return "R" + r
	case t == r:
		return "TSQL " + t
	default:
		return "T" + t + " R" + r
	}
}

func date(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("2006-01-02")
}

// channelFields are the columns of a channel listing.
func channelColumns() []field[domain.Channel] {
	type c = domain.Channel
	return []field[c]{
		{name: "id", id: true, text: func(c c) string { return c.ID }},
		{name: "name", text: func(c c) string { return c.Name }},
		{name: "rx", text: func(c c) string { return domain.FormatFrequencyMHz(c.ReceiveHz) }},
		{name: "tx", text: func(c c) string { return mhz(c.TransmitHz) }},
		{name: "duplex", text: domain.Duplex},
		{name: "offset", text: offsetMHz},
		{name: "tone", text: toneSummary},
		{name: "txtone", text: func(c c) string { return toneValue(c.TransmitTone) }},
		{name: "rxtone", text: func(c c) string { return toneValue(c.ReceiveTone) }},
		{name: "squelch", text: func(c c) string { return yesNo(c.ToneSquelch) }, json: func(c c) any { return c.ToneSquelch }},
		{name: "mode", text: func(c c) string { return c.Mode }},
		{name: "service", text: func(c c) string { return string(c.Service) }},
		{name: "rxonly", text: func(c c) string { return yesNo(c.RXOnly) }, json: func(c c) any { return c.RXOnly }},
		{name: "power", text: func(c c) string { return string(c.Power) }},
		{name: "skip", text: func(c c) string { return yesNo(c.Skip) }, json: func(c c) any { return c.Skip }},
		{name: "tags", text: func(c c) string { return strings.Join(c.Tags, ",") }, json: func(c c) any { return nonNil(c.Tags) }},
		{name: "shortnames", text: func(c c) string { return strings.Join(c.ShortNames, ",") }, json: func(c c) any { return nonNil(c.ShortNames) }},
		{name: "group", text: groupName},
		{name: "bandwidth", text: func(c c) string { return c.Bandwidth }},
		{name: "notes", text: func(c c) string { return c.Notes }},
		{name: "created", text: func(c c) string { return date(c.CreatedAt) }, json: func(c c) any { return c.CreatedAt }},
		{name: "updated", text: func(c c) string { return date(c.UpdatedAt) }, json: func(c c) any { return c.UpdatedAt }},
	}
}

func nonNil(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

// channelListing is channel list's output; its JSON without --fields is
// the full channel document, as channel show writes it.
var channelListing = listing[domain.Channel]{
	fields:   channelColumns(),
	defaults: []string{"id", "name", "rx", "duplex", "offset", "tone", "mode", "service", "tags"},
	full: func(cs []domain.Channel) any {
		out := make([]channelOut, len(cs))
		for i, c := range cs {
			out[i] = newChannelOut(c)
		}
		return out
	},
}

// memory is one row of radio channels list.
type memory struct {
	program.Memory
	entry domain.RadioEntry
}

func memoryColumns() []field[memory] {
	type m = memory
	channel := func(f field[domain.Channel]) field[m] {
		g := field[m]{name: f.name, id: f.id, text: func(m m) string { return f.text(m.Channel) }}
		if f.json != nil {
			g.json = func(m m) any { return f.json(m.Channel) }
		}
		return g
	}
	byName := map[string]field[domain.Channel]{}
	for _, f := range channelColumns() {
		byName[f.name] = f
	}
	return []field[m]{
		{name: "memory", text: func(m m) string { return mark(m.entry.SectionStart != nil, "§") + fmt.Sprint(m.Location) }, json: func(m m) any { return m.Location }},
		{name: "section_start", text: func(m m) string { return yesNo(m.entry.SectionStart != nil) }, json: func(m m) any { return m.entry.SectionStart != nil }},
		{name: "name", text: func(m m) string { return m.Channel.Name + mark(m.entry.Name != "", "*") }, json: func(m m) any { return m.Channel.Name }},
		{name: "fit", text: func(m m) string { return string(m.Name.Method) }},
		{name: "power", text: func(m m) string { return powerLabel(m.Memory) + mark(m.entry.Power != "", "*") }, json: func(m m) any { return powerLabel(m.Memory) }},
		{name: "scan", text: func(m m) string { return scanText(m.Channel.Skip) + mark(m.entry.Skip != nil, "*") }, json: func(m m) any { return !m.Channel.Skip }},
		channel(byName["rx"]), channel(byName["tx"]), channel(byName["duplex"]), channel(byName["offset"]),
		channel(byName["tone"]), channel(byName["mode"]), channel(byName["rxonly"]),
		{name: "exported", text: func(m m) string { return yesNo(!m.LeftOut) }, json: func(m m) any { return !m.LeftOut }},
		// The library channel's name, shown in text only when the radio's
		// differs.
		{name: "channel", text: func(m m) string {
			if m.Original.Name == m.Channel.Name {
				return ""
			}
			return m.Original.Name
		}, json: func(m m) any { return m.Original.Name }},
		{name: "id", id: true, text: func(m m) string { return m.Original.ID }},
	}
}

var memoryListing = listing[memory]{
	fields:   memoryColumns(),
	defaults: []string{"memory", "name", "fit", "power", "scan", "rx", "duplex", "offset", "tone", "channel", "id"},
}
