package importer

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/ryan/rflib/internal/domain"
	"github.com/ryan/rflib/internal/export"
)

// ParseJSON reads an export.Document backup. Row numbers are 1-based indexes
// into the document's channels array.
func ParseJSON(r io.Reader) (Batch, error) {
	var b Batch
	var doc export.Document
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return b, fmt.Errorf("read JSON backup: %w", err)
	}
	if doc.Format != export.DocumentFormat || doc.Version != export.DocumentVersion {
		return b, fmt.Errorf("unsupported backup: format %q version %d (want %q version %d)", doc.Format, doc.Version, export.DocumentFormat, export.DocumentVersion)
	}
	groups := make(map[string]domain.Group, len(doc.Groups))
	for _, g := range doc.Groups {
		group := domain.Group{ID: g.ID, Name: g.Name, Description: g.Description, Latitude: g.Latitude, Longitude: g.Longitude, RadiusKM: g.RadiusKM, Notes: g.Notes, CreatedAt: g.CreatedAt, UpdatedAt: g.UpdatedAt}
		if err := domain.ValidateGroup(group); err != nil {
			return b, fmt.Errorf("group %q: %w", g.ID, err)
		}
		groups[g.ID] = group
		b.Groups = append(b.Groups, group)
	}
	for i, rec := range doc.Channels {
		row := i + 1
		c := domain.Channel{ID: rec.ID, Name: rec.Name, ReceiveHz: rec.ReceiveHz, TransmitHz: rec.TransmitHz, Mode: rec.Mode, ToneSquelch: rec.ToneSquelch, Power: domain.PowerFromText(rec.Power), Bandwidth: rec.Bandwidth, Skip: rec.Skip, Tags: rec.Tags, ShortNames: rec.ShortNames, Notes: rec.Notes, CreatedAt: rec.CreatedAt, UpdatedAt: rec.UpdatedAt}
		var err error
		if c.Service, err = domain.ParseService(rec.Service); err != nil {
			b.fail(row, "%v", err)
			continue
		}
		c.RXOnly = rec.RXOnly
		if c.ReceiveTone, err = domain.ParseTone(rec.ReceiveTone); err != nil {
			b.fail(row, "receive_tone: %v", err)
			continue
		}
		if c.TransmitTone, err = domain.ParseTone(rec.TransmitTone); err != nil {
			b.fail(row, "transmit_tone: %v", err)
			continue
		}
		if rec.GroupID != nil {
			g, ok := groups[*rec.GroupID]
			if !ok {
				b.fail(row, "group_id %q is not in the backup's groups", *rec.GroupID)
				continue
			}
			c.Group = &g
		}
		b.finish(row, c)
	}
	return b, nil
}
