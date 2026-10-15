package app

import (
	"context"
	"fmt"
	"io"

	"github.com/ryan/rflib/internal/domain"
	"github.com/ryan/rflib/internal/export"
	"github.com/ryan/rflib/internal/naming"
	"github.com/ryan/rflib/internal/profile"
	"github.com/ryan/rflib/internal/query"
)

// ExportFormats are the library export formats.
var ExportFormats = []string{"csv", "json", "chirp", "profile"}

// ExportRequest is what to export and how. Query selects the channels (nil
// for all). Sort, Start, MaxNameLen, Dedupe, and SkipInvalid apply to the
// numbered formats, chirp and profile; Profile names the profile, by name
// or .yaml path (front ends that take input from others should allow
// names only).
type ExportRequest struct {
	Format      string
	Profile     string
	Query       query.Node
	Sort        string
	Start       *int
	MaxNameLen  int
	Dedupe      string
	SkipInvalid bool
}

// ExportResult is what an export wrote: how many channels, those left out
// as invalid, and names that came out the same.
type ExportResult struct {
	Count      int
	Skipped    []string
	Duplicates []string
}

// Export writes the library's channels in a file format. A failure can
// come after some output; callers that can't take back what they wrote
// should write to a buffer.
func (s *Service) Export(ctx context.Context, w io.Writer, req ExportRequest) (ExportResult, error) {
	var res ExportResult
	var cs []domain.Channel
	var err error
	if req.Query != nil {
		cs, err = s.FindChannels(ctx, req.Query)
	} else {
		cs, err = s.lib.ListChannels(ctx)
	}
	if err != nil {
		return res, err
	}
	var ex interface {
		export.Exporter
		export.Validator
	}
	var rules naming.Rules
	switch req.Format {
	case "csv":
		res.Count = len(cs)
		return res, export.GenericCSV{}.Export(w, cs)
	case "json":
		res.Count = len(cs)
		var recs *export.Records
		if s.info.Shared && req.Query == nil {
			if recs, err = s.sharedRecords(ctx); err != nil {
				return res, err
			}
		}
		return res, export.WriteJSON(w, cs, recs)
	case "chirp":
		start := 1
		if req.Start != nil {
			start = *req.Start
		}
		if start < 0 || req.MaxNameLen < 0 {
			return res, domain.Invalidf("the start and the name length must not be negative")
		}
		ex, rules = export.CHIRP{Start: start, MaxNameLen: req.MaxNameLen}, naming.Rules{MaxLen: req.MaxNameLen}
	case "profile":
		e, err := profile.Find(req.Profile)
		if err != nil {
			return res, err
		}
		if req.Start != nil && *req.Start < 0 {
			return res, domain.Invalidf("the start must not be negative")
		}
		ex, rules = profile.Exporter{Profile: e.Profile, Start: req.Start}, e.Profile.NameRules()
	default:
		return res, domain.Invalidf("unknown export format %q; use csv, json, chirp, or profile", req.Format)
	}
	dedupe, err := naming.ParseDedupe(orDefault(req.Dedupe, string(naming.DedupeNone)))
	if err != nil {
		return res, err
	}
	if err := domain.SortChannels(cs, orDefault(req.Sort, "name")); err != nil {
		return res, err
	}
	if req.SkipInvalid {
		kept := cs[:0]
		for _, c := range cs {
			if err := ex.Validate(c); err != nil {
				res.Skipped = append(res.Skipped, fmt.Sprintf("%q (%s MHz): %v", c.Name, domain.FormatFrequencyMHz(c.ReceiveHz), err))
				continue
			}
			kept = append(kept, c)
		}
		cs = kept
	}
	cs, results := naming.Apply(cs, rules, dedupe)
	for _, g := range naming.Duplicates(results) {
		res.Duplicates = append(res.Duplicates, fmt.Sprintf("%q (%d)", results[g[0]].Name, len(g)))
	}
	res.Count = len(cs)
	return res, ex.Export(w, cs)
}
