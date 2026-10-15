package domain

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Status is whether a channel is on the air.
type Status string

const (
	StatusActive Status = "active"
	// StatusOffAir is temporarily off the air, such as for maintenance.
	StatusOffAir Status = "off-air"
	// StatusRetired is gone for good, possibly replaced by another channel.
	StatusRetired Status = "retired"
)

var Statuses = []Status{StatusActive, StatusOffAir, StatusRetired}

func ParseStatus(v string) (Status, error) {
	switch s := Status(strings.ToLower(strings.TrimSpace(v))); s {
	case "":
		return StatusActive, nil
	case "offair", "off_air":
		return StatusOffAir, nil
	case StatusActive, StatusOffAir, StatusRetired:
		return s, nil
	}
	return "", Invalidf("unknown status %q; use active, off-air, or retired", v)
}

// OrActive is the status, with empty read as active.
func (s Status) OrActive() Status { return cmp.Or(s, StatusActive) }

// Link ties a library's channel to a shared channel. The channel's own
// Tags and Notes are the user's; the shared channel's are kept here.
type Link struct {
	SharedID string
	Tags     []string
	Notes    string
	// SeenRevision is the shared channel's revision the user last
	// acknowledged (see Channel.Changed).
	SeenRevision int
}

// Linked returns own, a link row, with the shared channel's fields filled
// in from shared.
func Linked(own, shared Channel) Channel {
	c := shared
	c.ID, c.Tags, c.Notes, c.Skip, c.Power, c.ShortNames = own.ID, own.Tags, own.Notes, own.Skip, own.Power, own.ShortNames
	c.CreatedAt, c.UpdatedAt, c.CopiedFrom = own.CreatedAt, own.UpdatedAt, ""
	seen := 0
	if own.Link != nil {
		seen = own.Link.SeenRevision
	}
	c.Link = &Link{SharedID: shared.ID, Tags: shared.Tags, Notes: shared.Notes, SeenRevision: seen}
	return c
}

// AllTags is the channel's tags with a link's shared tags, sorted.
func (c Channel) AllTags() []string {
	if c.Link == nil {
		return c.Tags
	}
	tags := NormalizeTags(append(slices.Clone(c.Link.Tags), c.Tags...))
	slices.Sort(tags)
	return tags
}

// AllNotes is a link's shared notes followed by the user's own, or a
// private channel's notes.
func (c Channel) AllNotes() string {
	if c.Link == nil || c.Link.Notes == "" {
		return c.Notes
	}
	if c.Notes == "" {
		return c.Link.Notes
	}
	return c.Link.Notes + "\n" + c.Notes
}

// Changed reports whether a link's shared channel changed since the user
// last acknowledged it.
func (c Channel) Changed() bool { return c.Link != nil && c.Revision > c.Link.SeenRevision }

// Revision is one version of a shared channel: the channel as it was then,
// and why it changed.
type Revision struct {
	Number  int
	At      time.Time
	Reason  string
	Author  string
	Channel Channel
}

// SharedChanges lists how the fields a shared channel owns differ from a
// to b, one "field: old → new" line each.
func SharedChanges(a, b Channel) []string {
	var out []string
	add := func(field, x, y string) {
		if x != y {
			out = append(out, fmt.Sprintf("%s: %s → %s", field, orNone(x), orNone(y)))
		}
	}
	freq := func(hz *int64) string {
		if hz == nil {
			return ""
		}
		return FormatFrequencyMHz(*hz)
	}
	loc := func(l *Location) string {
		if l == nil {
			return ""
		}
		s := fmt.Sprintf("%g,%g", l.Latitude, l.Longitude)
		if l.RadiusKM != nil {
			s += fmt.Sprintf(" within %g km", *l.RadiusKM)
		}
		return s
	}
	day := func(t *time.Time) string {
		if t == nil {
			return ""
		}
		return t.Format(time.DateOnly)
	}
	add("name", a.Name, b.Name)
	add("rx", FormatFrequencyMHz(a.ReceiveHz), FormatFrequencyMHz(b.ReceiveHz))
	add("tx", freq(a.TransmitHz), freq(b.TransmitHz))
	add("mode", a.Mode, b.Mode)
	add("service", string(a.Service), string(b.Service))
	add("rx only", fmt.Sprint(a.RXOnly), fmt.Sprint(b.RXOnly))
	add("receive tone", FormatTone(a.ReceiveTone), FormatTone(b.ReceiveTone))
	add("transmit tone", FormatTone(a.TransmitTone), FormatTone(b.TransmitTone))
	add("tone squelch", fmt.Sprint(a.ToneSquelch), fmt.Sprint(b.ToneSquelch))
	add("bandwidth", a.Bandwidth, b.Bandwidth)
	add("location", loc(a.Location), loc(b.Location))
	add("networks", formatNetworkIDs(a.Networks), formatNetworkIDs(b.Networks))
	add("organizations", formatOrgRefs(a.Orgs), formatOrgRefs(b.Orgs))
	add("tags", strings.Join(a.Tags, ","), strings.Join(b.Tags, ","))
	add("notes", a.Notes, b.Notes)
	add("status", string(a.Status.OrActive()), string(b.Status.OrActive()))
	add("status note", a.StatusNote, b.StatusNote)
	add("back on", day(a.BackOn), day(b.BackOn))
	add("replaced by", a.ReplacedBy, b.ReplacedBy)
	return out
}

func orNone(s string) string { return cmp.Or(s, "(none)") }

// CuratedTags are the tags shared channels may carry, each with what it
// means. Personal tags are free-form; a personal tag with a curated name
// means the same thing.
var CuratedTags = []struct{ Name, Meaning string }{
	{"calling", "a calling frequency: make contact here, then move"},
	{"ares", "used by ARES (Amateur Radio Emergency Service)"},
	{"races", "used by RACES (Radio Amateur Civil Emergency Service)"},
	{"skywarn", "used for SKYWARN severe-weather spotting"},
	{"net", "hosts a regular scheduled net"},
	{"emergency", "set aside for emergency traffic"},
	{"travel", "useful to travelers, such as a highway or travel net"},
}

// ValidateSharedTags checks a shared channel's tags are all curated.
func ValidateSharedTags(tags []string) error {
	for _, t := range tags {
		if !slices.ContainsFunc(CuratedTags, func(c struct{ Name, Meaning string }) bool { return c.Name == t }) {
			return Invalidf("tag %q isn't one of the shared tags (see rflib tag list)", t)
		}
	}
	return nil
}

// ProgrammedKey is what a radio is programmed with: frequencies, tones, and
// tone squelch. Channels with the same key sound the same on the air.
func ProgrammedKey(c Channel) string {
	tx := "rx-only"
	if c.TransmitHz != nil {
		tx = strconv.FormatInt(*c.TransmitHz, 10)
	}
	return strings.Join([]string{strconv.FormatInt(c.ReceiveHz, 10), tx,
		FormatTone(c.ReceiveTone), FormatTone(c.TransmitTone), strconv.FormatBool(c.ToneSquelch)}, "\x00")
}
