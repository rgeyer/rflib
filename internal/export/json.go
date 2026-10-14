package export

import (
	"encoding/json"
	"io"
	"time"

	"github.com/ryan/rflib/internal/domain"
)

// DocumentFormat and DocumentVersion identify an rflib JSON backup. Bump the
// version on any incompatible change to Document.
const (
	DocumentFormat  = "rflib"
	DocumentVersion = 2
)

// Document is the full-fidelity JSON backup: every stored field, including IDs
// and timestamps, so a restore reproduces the library exactly. Version 1
// backups also had groups, which importing turns into locations and tags.
type Document struct {
	Format   string          `json:"format"`
	Version  int             `json:"version"`
	Groups   []GroupRecord   `json:"groups,omitempty"`
	Channels []ChannelRecord `json:"channels"`
}

// GroupRecord is a group in a version 1 backup.
type GroupRecord struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Latitude    *float64  `json:"latitude,omitempty"`
	Longitude   *float64  `json:"longitude,omitempty"`
	RadiusKM    *float64  `json:"radius_km,omitempty"`
	Notes       string    `json:"notes"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ChannelRecord stores frequencies as exact integer Hz and tones in the
// library's normalized syntax (see domain.FormatTone).
type ChannelRecord struct {
	ID string `json:"id"`
	// GroupID is the channel's group in a version 1 backup.
	GroupID      *string   `json:"group_id,omitempty"`
	Name         string    `json:"name"`
	ReceiveHz    int64     `json:"receive_hz"`
	TransmitHz   *int64    `json:"transmit_hz,omitempty"`
	Mode         string    `json:"mode"`
	Service      string    `json:"service,omitempty"`
	RXOnly       bool      `json:"rx_only,omitempty"`
	ReceiveTone  string    `json:"receive_tone"`
	TransmitTone string    `json:"transmit_tone"`
	ToneSquelch  bool      `json:"tone_squelch"`
	Power        string    `json:"power"`
	Bandwidth    string    `json:"bandwidth"`
	Skip         bool      `json:"skip"`
	Tags         []string  `json:"tags"`
	ShortNames   []string  `json:"short_names,omitempty"`
	Notes        string    `json:"notes"`
	Latitude     *float64  `json:"latitude,omitempty"`
	Longitude    *float64  `json:"longitude,omitempty"`
	RadiusKM     *float64  `json:"radius_km,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// WriteJSON writes channels as an indented Document.
func WriteJSON(w io.Writer, channels []domain.Channel) error {
	doc := Document{Format: DocumentFormat, Version: DocumentVersion, Channels: []ChannelRecord{}}
	for _, c := range channels {
		tags := c.Tags
		if tags == nil {
			tags = []string{}
		}
		var lat, lon, radius *float64
		if l := c.Location; l != nil {
			lat, lon, radius = &l.Latitude, &l.Longitude, l.RadiusKM
		}
		doc.Channels = append(doc.Channels, ChannelRecord{
			Latitude: lat, Longitude: lon, RadiusKM: radius,
			ID: c.ID, Name: c.Name, ReceiveHz: c.ReceiveHz, TransmitHz: c.TransmitHz, Mode: c.Mode, Service: string(c.Service), RXOnly: c.RXOnly,
			ReceiveTone: domain.FormatTone(c.ReceiveTone), TransmitTone: domain.FormatTone(c.TransmitTone),
			ToneSquelch: c.ToneSquelch, Power: string(c.Power), Bandwidth: c.Bandwidth, Skip: c.Skip, Tags: tags, ShortNames: c.ShortNames, Notes: c.Notes,
			CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
		})
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(doc)
}
