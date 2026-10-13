package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/ryan/rflib/internal/domain"
)

type groupOut struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Latitude    *float64 `json:"latitude,omitempty"`
	Longitude   *float64 `json:"longitude,omitempty"`
	RadiusKM    *float64 `json:"radius_km,omitempty"`
	Notes       string   `json:"notes"`
}

type channelOut struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Group       *groupOut `json:"group,omitempty"`
	ReceiveHz   int64     `json:"receive_hz"`
	ReceiveMHz  string    `json:"receive_mhz"`
	TransmitHz  *int64    `json:"transmit_hz,omitempty"`
	TransmitMHz *string   `json:"transmit_mhz,omitempty"`
	Mode        string    `json:"mode"`
	Service     string    `json:"service"`
	RXOnly      bool      `json:"rx_only"`
	RXTone      string    `json:"rx_tone"`
	TXTone      string    `json:"tx_tone"`
	ToneSquelch bool      `json:"tone_squelch"`
	Power       string    `json:"power"`
	Bandwidth   string    `json:"bandwidth"`
	Skip        bool      `json:"skip"`
	Tags        []string  `json:"tags"`
	Notes       string    `json:"notes"`
}

func newGroupOut(g domain.Group) groupOut {
	return groupOut{ID: g.ID, Name: g.Name, Description: g.Description, Latitude: g.Latitude, Longitude: g.Longitude, RadiusKM: g.RadiusKM, Notes: g.Notes}
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
		Power:       c.Power,
		Bandwidth:   c.Bandwidth,
		Skip:        c.Skip,
		Tags:        c.Tags,
		Notes:       c.Notes,
	}
	if o.Tags == nil {
		o.Tags = []string{}
	}
	if c.TransmitHz != nil {
		mhz := domain.FormatFrequencyMHz(*c.TransmitHz)
		o.TransmitMHz = &mhz
	}
	if c.Group != nil {
		g := newGroupOut(*c.Group)
		o.Group = &g
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

func writeGroupList(out io.Writer, groups []domain.Group, format string) error {
	if format == "json" {
		items := make([]groupOut, len(groups))
		for i, g := range groups {
			items[i] = newGroupOut(g)
		}
		return writeJSON(out, items)
	}
	for _, g := range groups {
		fmt.Fprintf(out, "%s\t%s\t%s\n", g.ID, g.Name, formatCoordinates(g))
	}
	return nil
}

func writeGroup(out io.Writer, g domain.Group, format string) error {
	if format == "json" {
		return writeJSON(out, newGroupOut(g))
	}
	fmt.Fprintf(out, "%s\nname: %s\ncoordinates: %s\nradius: %s\ndescription: %s\nnotes: %s\n", g.ID, g.Name, formatCoordinates(g), optionalKM(g.RadiusKM), g.Description, g.Notes)
	return nil
}

func writeChannelList(out io.Writer, channels []domain.Channel, format string) error {
	if format == "json" {
		items := make([]channelOut, len(channels))
		for i, c := range channels {
			items[i] = newChannelOut(c)
		}
		return writeJSON(out, items)
	}
	for _, c := range channels {
		fmt.Fprintf(out, "%s\t%s\t%s\t%s\n", c.ID, c.Name, domain.FormatFrequencyMHz(c.ReceiveHz), groupName(c))
	}
	return nil
}

func writeChannel(out io.Writer, c domain.Channel, format string) error {
	if format == "json" {
		return writeJSON(out, newChannelOut(c))
	}
	fmt.Fprintf(out, "%s\nname: %s\ngroup: %s\nservice: %s\nrx: %s MHz\ntx: %s\nmode: %s\nreceive tone: %s\ntransmit tone: %s\ntone squelch: %t\npower: %s\nbandwidth: %s\nskip: %t\ntags: %s\nnotes: %s\n",
		c.ID, c.Name, groupName(c), serviceName(c.Service), domain.FormatFrequencyMHz(c.ReceiveHz), txText(c), c.Mode,
		domain.FormatTone(c.ReceiveTone), domain.FormatTone(c.TransmitTone), c.ToneSquelch, c.Power, c.Bandwidth, c.Skip,
		strings.Join(c.Tags, ","), c.Notes)
	return nil
}

func optionalFloat(v *float64) string {
	if v == nil {
		return ""
	}
	return strconv.FormatFloat(*v, 'f', 6, 64)
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

func formatCoordinates(g domain.Group) string {
	if g.Latitude == nil {
		return ""
	}
	return optionalFloat(g.Latitude) + "," + optionalFloat(g.Longitude)
}

func groupName(c domain.Channel) string {
	if c.Group == nil {
		return ""
	}
	return c.Group.Name
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
