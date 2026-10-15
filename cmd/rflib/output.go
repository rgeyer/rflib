package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/ryan/rflib/internal/domain"
)

type channelOut struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	ReceiveHz   int64           `json:"receive_hz"`
	ReceiveMHz  string          `json:"receive_mhz"`
	TransmitHz  *int64          `json:"transmit_hz,omitempty"`
	TransmitMHz *string         `json:"transmit_mhz,omitempty"`
	Mode        string          `json:"mode"`
	Service     string          `json:"service"`
	RXOnly      bool            `json:"rx_only"`
	RXTone      string          `json:"rx_tone"`
	TXTone      string          `json:"tx_tone"`
	ToneSquelch bool            `json:"tone_squelch"`
	Power       string          `json:"power"`
	Bandwidth   string          `json:"bandwidth"`
	Skip        bool            `json:"skip"`
	Tags        []string        `json:"tags"`
	ShortNames  []string        `json:"short_names"`
	Notes       string          `json:"notes"`
	Latitude    *float64        `json:"latitude,omitempty"`
	Longitude   *float64        `json:"longitude,omitempty"`
	RadiusKM    *float64        `json:"radius_km,omitempty"`
	Status      string          `json:"status"`
	StatusNote  string          `json:"status_note,omitempty"`
	BackOn      string          `json:"back_on,omitempty"`
	ReplacedBy  string          `json:"replaced_by,omitempty"`
	Revision    int             `json:"revision,omitempty"`
	CopiedFrom  string          `json:"copied_from,omitempty"`
	Link        *linkOut        `json:"link,omitempty"`
	Networks    []string        `json:"networks,omitempty"`
	Orgs        []domain.OrgRef `json:"organizations,omitempty"`
}

// linkOut is a link's shared channel: its ID, its tags and notes, and
// whether it changed since the user last acknowledged it.
type linkOut struct {
	SharedID     string   `json:"shared_id"`
	Tags         []string `json:"tags"`
	Notes        string   `json:"notes"`
	SeenRevision int      `json:"seen_revision"`
	Changed      bool     `json:"changed"`
}

func newChannelOut(c domain.Channel) channelOut {
	o := channelOut{
		ID:          c.ID,
		Name:        c.Name,
		ReceiveHz:   c.ReceiveHz,
		ReceiveMHz:  domain.FormatFrequencyMHz(c.ReceiveHz),
		TransmitHz:  c.TransmitHz,
		Mode:        c.Mode,
		Service:     string(c.Service),
		RXOnly:      c.RXOnly,
		RXTone:      domain.FormatTone(c.ReceiveTone),
		TXTone:      domain.FormatTone(c.TransmitTone),
		ToneSquelch: c.ToneSquelch,
		Power:       string(c.Power),
		Bandwidth:   c.Bandwidth,
		Skip:        c.Skip,
		Tags:        c.Tags,
		ShortNames:  c.ShortNames,
		Notes:       c.Notes,
	}
	if o.Tags == nil {
		o.Tags = []string{}
	}
	if o.ShortNames == nil {
		o.ShortNames = []string{}
	}
	if c.TransmitHz != nil {
		mhz := domain.FormatFrequencyMHz(*c.TransmitHz)
		o.TransmitMHz = &mhz
	}
	if l := c.Location; l != nil {
		o.Latitude, o.Longitude, o.RadiusKM = &l.Latitude, &l.Longitude, l.RadiusKM
	}
	o.Status, o.StatusNote, o.ReplacedBy, o.Revision, o.CopiedFrom = string(c.Status.OrActive()), c.StatusNote, c.ReplacedBy, c.Revision, c.CopiedFrom
	if c.BackOn != nil {
		o.BackOn = c.BackOn.Format("2006-01-02")
	}
	o.Networks, o.Orgs = networkStrings(c.Networks), c.Orgs
	if l := c.Link; l != nil {
		o.Link = &linkOut{SharedID: l.SharedID, Tags: nonNil(l.Tags), Notes: l.Notes, SeenRevision: l.SeenRevision, Changed: c.Changed()}
	}
	return o
}

func writeJSON(out io.Writer, v any) error {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func checkFormat(format string) error {
	if format != "text" && format != "json" {
		return fmt.Errorf("unknown format %q; use text or json", format)
	}
	return nil
}

func writeChannel(out io.Writer, c domain.Channel, format string) error {
	if format == "json" {
		return writeJSON(out, newChannelOut(c))
	}
	fmt.Fprintf(out, "%s\nname: %s\nshort names: %s\nlocation: %s\nservice: %s\nrx: %s MHz\ntx: %s\nmode: %s\nreceive tone: %s\ntransmit tone: %s\ntone squelch: %t\npower: %s\nbandwidth: %s\nskip: %t\ntags: %s\nnotes: %s\n",
		c.ID, c.Name, strings.Join(c.ShortNames, ", "), locationText(c.Location), serviceName(c.Service), domain.FormatFrequencyMHz(c.ReceiveHz), txText(c), c.Mode,
		domain.FormatTone(c.ReceiveTone), domain.FormatTone(c.TransmitTone), c.ToneSquelch, powerName(c.Power), c.Bandwidth, c.Skip,
		strings.Join(c.Tags, ","), c.Notes)
	fmt.Fprintf(out, "status: %s\n", statusText(c))
	if len(c.Networks) > 0 {
		fmt.Fprintf(out, "networks: %s\n", strings.Join(networkStrings(c.Networks), ", "))
	}
	if len(c.Orgs) > 0 {
		fmt.Fprintf(out, "organizations: %s\n", orgRefsText(c.Orgs))
	}
	if l := c.Link; l != nil {
		changed := ""
		if c.Changed() {
			changed = "; changed since you acknowledged it (see rflib shared changes)"
		}
		fmt.Fprintf(out, "linked to: shared channel %s, revision %d%s\nshared tags: %s\nshared notes: %s\n", l.SharedID, c.Revision, changed, strings.Join(l.Tags, ","), l.Notes)
	}
	if c.CopiedFrom != "" {
		fmt.Fprintf(out, "copied from: shared channel %s\n", c.CopiedFrom)
	}
	return nil
}

func networkStrings(ns []domain.NetworkID) []string {
	var out []string
	for _, n := range ns {
		out = append(out, n.String())
	}
	return out
}

// orgRefsText lists organizations with their roles: "SBARC (operator),
// GRONK (partner)".
func orgRefsText(rs []domain.OrgRef) string {
	s := make([]string, len(rs))
	for i, r := range rs {
		s[i] = r.Name + " (" + string(r.Role) + ")"
	}
	return strings.Join(s, ", ")
}

// statusText is a channel's status for reading: off-air with its note and
// expected return, retired with its replacement.
func statusText(c domain.Channel) string {
	s := string(c.Status.OrActive())
	if c.StatusNote != "" {
		s += " (" + c.StatusNote + ")"
	}
	if c.BackOn != nil {
		s += ", expected back " + c.BackOn.Format("2006-01-02")
	}
	if c.ReplacedBy != "" {
		s += ", replaced by " + c.ReplacedBy
	}
	return s
}

func powerName(p domain.PowerIntent) string {
	if p == domain.PowerUnset {
		return "unset"
	}
	return string(p)
}

func optionalKM(v *float64) string {
	if v == nil {
		return ""
	}
	return strconv.FormatFloat(*v, 'f', -1, 64) + " km"
}

func optionalFreq(v *int64) string {
	if v == nil {
		return "receive only"
	}
	return domain.FormatFrequencyMHz(*v) + " MHz"
}

// locationText is a location for reading: coordinates, then the radius.
func locationText(l *domain.Location) string {
	if l == nil {
		return ""
	}
	if l.RadiusKM == nil {
		return coordinates(l)
	}
	return coordinates(l) + " within " + optionalKM(l.RadiusKM)
}

// coordinates is a location's latitude and longitude, as near: takes them.
func coordinates(l *domain.Location) string {
	if l == nil {
		return ""
	}
	return strconv.FormatFloat(l.Latitude, 'f', -1, 64) + "," + strconv.FormatFloat(l.Longitude, 'f', -1, 64)
}

func serviceName(s domain.Service) string {
	if s == domain.ServiceUnspecified {
		return "unspecified"
	}
	return string(s)
}

func txText(c domain.Channel) string {
	if c.RXOnly && c.TransmitHz != nil {
		return optionalFreq(c.TransmitHz) + " (receive only; never transmitted)"
	}
	return optionalFreq(c.TransmitHz)
}
