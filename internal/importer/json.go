package importer

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

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
	if doc.Format != export.DocumentFormat || doc.Version < 1 || doc.Version > export.DocumentVersion {
		return b, domain.Invalidf("unsupported backup: format %q version %d (want %q version 1 to %d)", doc.Format, doc.Version, export.DocumentFormat, export.DocumentVersion)
	}
	// Version 1 backups have groups, which become tags and locations.
	groups := make(map[string]export.GroupRecord, len(doc.Groups))
	for _, g := range doc.Groups {
		groups[g.ID] = g
	}
	for _, o := range doc.Organizations {
		o.Organization.ID, o.Organization.CreatedAt, o.Organization.UpdatedAt = o.ID, o.CreatedAt, o.UpdatedAt
		b.Shared.Orgs = append(b.Shared.Orgs, o.Organization)
	}
	for _, x := range doc.Systems {
		x.System.ID, x.System.CreatedAt, x.System.UpdatedAt = x.ID, x.CreatedAt, x.UpdatedAt
		b.Shared.Systems = append(b.Shared.Systems, x.System)
	}
	for _, p := range doc.Plans {
		p.Plan.ID, p.Plan.CreatedAt, p.Plan.UpdatedAt = p.ID, p.CreatedAt, p.UpdatedAt
		b.Shared.Plans = append(b.Shared.Plans, p.Plan)
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
		if c.Location, err = domain.NewLocation(rec.Latitude, rec.Longitude, rec.RadiusKM); err != nil {
			b.fail(row, "location: %v", err)
			continue
		}
		if c.Status, err = domain.ParseStatus(rec.Status); err != nil {
			b.fail(row, "%v", err)
			continue
		}
		c.StatusNote, c.ReplacedBy, c.CopiedFrom = rec.StatusNote, rec.ReplacedBy, rec.CopiedFrom
		if rec.BackOn != "" {
			t, err := time.Parse(time.DateOnly, rec.BackOn)
			if err != nil {
				b.fail(row, "back_on: %v", err)
				continue
			}
			c.BackOn = &t
		}
		if l := rec.Link; l != nil {
			c.Link = &domain.Link{SharedID: l.SharedID, SeenRevision: l.SeenRevision, Tags: l.Tags, Notes: l.Notes}
		}
		if c.Networks, err = domain.ParseNetworkIDs(rec.Networks); err != nil {
			b.fail(row, "networks: %v", err)
			continue
		}
		c.Orgs = rec.Orgs
		if rec.GroupID != nil {
			g, ok := groups[*rec.GroupID]
			if !ok {
				b.fail(row, "group_id %q is not in the backup's groups", *rec.GroupID)
				continue
			}
			if err := ungroup(&c, g.Name, g.Latitude, g.Longitude, g.RadiusKM); err != nil {
				b.fail(row, "group %q: %v", g.Name, err)
				continue
			}
		}
		b.finish(row, c)
	}
	return b, nil
}
