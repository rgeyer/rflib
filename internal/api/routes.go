package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/ryan/rflib/internal/app"
	"github.com/ryan/rflib/internal/domain"
	"github.com/ryan/rflib/internal/importer"
	"github.com/ryan/rflib/internal/model"
	"github.com/ryan/rflib/internal/profile"
	"github.com/ryan/rflib/internal/program"
	"github.com/ryan/rflib/internal/query"
	"github.com/ryan/rflib/internal/seed"
)

func (s *server) routes() []route {
	L := "/libraries/{lib}"
	R := L + "/radios/{radio}"
	return []route{
		{"GET", "/health", s.health, true},
		{"GET", "/openapi.yaml", s.spec, true},

		{"GET", "/libraries", s.listLibraries, false},
		{"GET", L, s.lib(s.getLibrary), false},

		{"GET", L + "/channels", s.lib(s.listChannels), false},
		{"POST", L + "/channels", s.lib(s.createChannel), false},
		{"GET", L + "/channels/{id}", s.lib(s.getChannel), false},
		{"PATCH", L + "/channels/{id}", s.lib(s.updateChannel), false},
		{"DELETE", L + "/channels/{id}", s.lib(s.deleteChannel), false},
		{"GET", L + "/channels/{id}/history", s.lib(s.channelHistory), false},
		{"POST", L + "/channels/{id}/status", s.lib(s.setStatus), false},
		{"POST", L + "/channels/bulk-update", s.lib(s.bulkUpdate), false},
		{"POST", L + "/channels/bulk-delete", s.lib(s.bulkDelete), false},
		{"POST", L + "/channels/link", s.lib(s.linkChannels), false},
		{"POST", L + "/channels/copy", s.lib(s.copyChannels), false},

		{"POST", L + "/shared/convert", s.lib(s.convert), false},
		{"GET", L + "/shared/changes", s.lib(s.changes), false},
		{"POST", L + "/shared/ack", s.lib(s.ack), false},
		{"POST", L + "/shared/relink", s.lib(s.relink), false},
		{"GET", L + "/feed", s.lib(s.feed), false},

		{"POST", L + "/imports/{format}", s.lib(s.importFile), false},
		{"GET", L + "/exports/{format}", s.lib(s.exportFile), false},

		{"GET", L + "/searches", s.lib(s.listSearches), false},
		{"POST", L + "/searches", s.lib(s.createSearch), false},
		{"GET", L + "/searches/{name}", s.lib(s.getSearch), false},
		{"PATCH", L + "/searches/{name}", s.lib(s.updateSearch), false},
		{"DELETE", L + "/searches/{name}", s.lib(s.deleteSearch), false},

		{"GET", L + "/radios", s.lib(s.listRadios), false},
		{"POST", L + "/radios", s.lib(s.createRadio), false},
		{"GET", R, s.lib(s.getRadio), false},
		{"PATCH", R, s.lib(s.updateRadio), false},
		{"DELETE", R, s.lib(s.deleteRadio), false},
		{"GET", R + "/memories", s.lib(s.memories), false},
		{"POST", R + "/channels", s.lib(s.addRadioChannels), false},
		{"POST", R + "/channels/remove", s.lib(s.removeRadioChannels), false},
		{"POST", R + "/channels/set", s.lib(s.setRadioChannels), false},
		{"PATCH", R + "/channels/{id}", s.lib(s.setRadioChannel), false},
		{"POST", R + "/channels/{id}/move", s.lib(s.moveRadioChannel), false},
		{"GET", R + "/sources", s.lib(s.listSources), false},
		{"POST", R + "/sources", s.lib(s.addSource), false},
		{"POST", R + "/sources/preview", s.lib(s.previewSource), false},
		{"PATCH", R + "/sources/{n}", s.lib(s.updateSource), false},
		{"DELETE", R + "/sources/{n}", s.lib(s.removeSource), false},
		{"POST", R + "/sources/{n}/move", s.lib(s.moveSource), false},
		{"POST", R + "/sync", s.lib(s.syncRadio), false},
		{"GET", R + "/export", s.lib(s.exportRadio), false},

		{"GET", "/organizations", s.recordHandler(s.listOrgs), false},
		{"POST", "/organizations", s.recordHandler(s.createOrg), false},
		{"GET", "/organizations/{org}", s.recordHandler(s.getOrg), false},
		{"PATCH", "/organizations/{org}", s.recordHandler(s.updateOrg), false},
		{"DELETE", "/organizations/{org}", s.recordHandler(s.deleteOrg), false},
		{"GET", "/organizations/{org}/history", s.recordHandler(s.history(app.KindOrganization, "org")), false},
		{"GET", "/systems", s.recordHandler(s.listSystems), false},
		{"POST", "/systems", s.recordHandler(s.createSystem), false},
		{"GET", "/systems/{system}", s.recordHandler(s.getSystem), false},
		{"PATCH", "/systems/{system}", s.recordHandler(s.updateSystem), false},
		{"DELETE", "/systems/{system}", s.recordHandler(s.deleteSystem), false},
		{"GET", "/systems/{system}/history", s.recordHandler(s.history(app.KindSystem, "system")), false},
		{"GET", "/plans", s.recordHandler(s.listPlans), false},
		{"POST", "/plans", s.recordHandler(s.createPlan), false},
		{"GET", "/plans/{plan}", s.recordHandler(s.getPlan), false},
		{"PATCH", "/plans/{plan}", s.recordHandler(s.updatePlan), false},
		{"GET", "/plans/{plan}/history", s.recordHandler(s.history(app.KindPlan, "plan")), false},

		{"GET", "/models", s.listModels, false},
		{"GET", "/models/{name}", s.getModel, false},
		{"GET", "/profiles", s.listProfiles, false},
		{"GET", "/seeds", s.listSeeds, false},
		{"GET", "/seeds/{seed}", s.getSeed, false},
		{"POST", "/seeds/{seed}/update", s.updateSeed, false},
		{"GET", "/tags", s.listTags, false},
	}
}

// --- service ---

func (s *server) health(w http.ResponseWriter, r *http.Request) error {
	if err := s.st.Ready(ctx(r)); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	return nil
}

func (s *server) spec(w http.ResponseWriter, _ *http.Request) error {
	w.Header().Set("Content-Type", "application/yaml")
	_, err := w.Write(s.opts.Spec)
	return err
}

// --- libraries ---

func (s *server) listLibraries(w http.ResponseWriter, r *http.Request) error {
	libs, err := s.st.ListLibraries(ctx(r))
	if err != nil {
		return err
	}
	out := make([]Library, len(libs))
	for i, l := range libs {
		out[i] = newLibrary(l)
	}
	writeJSON(w, http.StatusOK, map[string]any{"libraries": out})
	return nil
}

func (s *server) getLibrary(w http.ResponseWriter, _ *http.Request, a *app.Service) error {
	writeJSON(w, http.StatusOK, newLibrary(a.Library()))
	return nil
}

// --- channels ---

// channelResult is a saved channel and its band-plan warnings.
type channelResult struct {
	Channel  Channel  `json:"channel"`
	Warnings []string `json:"warnings"`
}

func (s *server) listChannels(w http.ResponseWriter, r *http.Request, a *app.Service) error {
	var cs []domain.Channel
	var err error
	if q := r.URL.Query().Get("q"); strings.TrimSpace(q) != "" {
		cs, err = a.Find(ctx(r), q)
	} else {
		cs, err = a.ListChannels(ctx(r))
	}
	if err != nil {
		return err
	}
	total := len(cs)
	offset, err := queryInt(r, "offset")
	if err != nil {
		return err
	}
	limit, err := queryInt(r, "limit")
	if err != nil {
		return err
	}
	if offset != nil {
		if *offset < 0 {
			return badRequest("offset must not be negative")
		}
		cs = cs[min(*offset, len(cs)):]
	}
	if limit != nil {
		if *limit < 0 {
			return badRequest("limit must not be negative")
		}
		cs = cs[:min(*limit, len(cs))]
	}
	writeJSON(w, http.StatusOK, map[string]any{"channels": newChannels(cs), "total": total})
	return nil
}

func (s *server) createChannel(w http.ResponseWriter, r *http.Request, a *app.Service) error {
	var raw map[string]json.RawMessage
	if err := readJSON(r, &raw); err != nil {
		return err
	}
	none := domain.Tone{Kind: domain.ToneNone}
	c := domain.Channel{Mode: "FM", ReceiveTone: none, TransmitTone: none}
	if err := applyChannel(&c, raw); err != nil {
		return err
	}
	c, warnings, err := a.CreateChannel(ctx(r), c)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, channelResult{newChannel(c), nonNil(warnings)})
	return nil
}

func (s *server) getChannel(w http.ResponseWriter, r *http.Request, a *app.Service) error {
	c, err := a.GetChannel(ctx(r), r.PathValue("id"))
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, newChannel(c))
	return nil
}

// updateChannel changes the fields given. On the shared library it's a
// maintainer correction and needs a reason.
func (s *server) updateChannel(w http.ResponseWriter, r *http.Request, a *app.Service) error {
	var raw map[string]json.RawMessage
	if err := readJSON(r, &raw); err != nil {
		return err
	}
	var reason string
	if v, ok := raw["reason"]; ok {
		if err := json.Unmarshal(v, &reason); err != nil {
			return badRequest("reason: %v", err)
		}
		delete(raw, "reason")
	}
	c, err := a.GetChannel(ctx(r), r.PathValue("id"))
	if err != nil {
		return err
	}
	if err := applyChannel(&c, raw); err != nil {
		return err
	}
	var warnings []string
	if a.Library().Shared {
		if strings.TrimSpace(reason) == "" {
			return badRequest("a change to a shared channel needs a reason")
		}
		c, warnings, err = a.UpdateShared(ctx(r), c, reason)
	} else {
		c, warnings, err = a.UpdateChannel(ctx(r), c)
	}
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, channelResult{newChannel(c), nonNil(warnings)})
	return nil
}

func (s *server) deleteChannel(w http.ResponseWriter, r *http.Request, a *app.Service) error {
	if err := a.DeleteChannel(ctx(r), r.PathValue("id")); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *server) channelHistory(w http.ResponseWriter, r *http.Request, a *app.Service) error {
	_, revs, err := a.History(ctx(r), r.PathValue("id"))
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"revisions": newRevisions(revs)})
	return nil
}

type statusInput struct {
	Status     string  `json:"status"`
	Note       string  `json:"note"`
	BackOn     *string `json:"back_on"`
	ReplacedBy string  `json:"replaced_by"`
	Reason     string  `json:"reason"`
}

func (s *server) setStatus(w http.ResponseWriter, r *http.Request, a *app.Service) error {
	var in statusInput
	if err := readJSON(r, &in); err != nil {
		return err
	}
	if strings.TrimSpace(in.Reason) == "" {
		return badRequest("a status change needs a reason")
	}
	st, err := domain.ParseStatus(in.Status)
	if err != nil {
		return err
	}
	back, err := date(in.BackOn)
	if err != nil {
		return err
	}
	c, err := a.SetStatus(ctx(r), r.PathValue("id"), app.StatusChange{Status: st, Note: in.Note, BackOn: back, ReplacedBy: in.ReplacedBy}, in.Reason)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, newChannel(c))
	return nil
}

type bulkUpdateInput struct {
	Select Selection   `json:"select"`
	Edit   ChannelEdit `json:"edit"`
	DryRun bool        `json:"dry_run"`
}

// channelsResult is channels a bulk operation touched, with warnings.
type channelsResult struct {
	Channels []Channel `json:"channels"`
	Warnings []string  `json:"warnings"`
}

func (s *server) bulkUpdate(w http.ResponseWriter, r *http.Request, a *app.Service) error {
	var in bulkUpdateInput
	if err := readJSON(r, &in); err != nil {
		return err
	}
	sel, err := in.Select.app()
	if err != nil {
		return err
	}
	edit, err := in.Edit.app()
	if err != nil {
		return err
	}
	var cs []domain.Channel
	var warnings []string
	if in.DryRun {
		if cs, err = a.Select(ctx(r), sel); err != nil {
			return err
		}
		for i := range cs {
			before := cs[i]
			edit.Apply(&cs[i])
			if err := app.LinkedEdit(before, cs[i]); err != nil {
				return err
			}
		}
	} else if cs, warnings, err = a.UpdateChannels(ctx(r), sel, edit); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, channelsResult{newChannels(cs), nonNil(warnings)})
	return nil
}

type selectInput struct {
	Select Selection `json:"select"`
	DryRun bool      `json:"dry_run"`
}

func (s *server) bulkDelete(w http.ResponseWriter, r *http.Request, a *app.Service) error {
	var in selectInput
	if err := readJSON(r, &in); err != nil {
		return err
	}
	sel, err := in.Select.app()
	if err != nil {
		return err
	}
	var cs []domain.Channel
	if in.DryRun {
		cs, err = a.Select(ctx(r), sel)
	} else {
		cs, err = a.DeleteChannels(ctx(r), sel)
	}
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, channelsResult{newChannels(cs), []string{}})
	return nil
}

type linkInput struct {
	Select Selection `json:"select"`
	Tags   []string  `json:"tags"`
}

func (s *server) linkChannels(w http.ResponseWriter, r *http.Request, a *app.Service) error {
	var in linkInput
	if err := readJSON(r, &in); err != nil {
		return err
	}
	sel, err := in.Select.app()
	if err != nil {
		return err
	}
	res, err := a.LinkChannels(ctx(r), sel, in.Tags)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"linked": newChannels(res.Linked), "already": newChannels(res.Already), "link_ids": nonNil(res.LinkIDs)})
	return nil
}

func (s *server) copyChannels(w http.ResponseWriter, r *http.Request, a *app.Service) error {
	var in selectInput
	if err := readJSON(r, &in); err != nil {
		return err
	}
	sel, err := in.Select.app()
	if err != nil {
		return err
	}
	cs, err := a.CopyChannels(ctx(r), sel)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, channelsResult{newChannels(cs), []string{}})
	return nil
}

// --- shared channels ---

func (s *server) convert(w http.ResponseWriter, r *http.Request, a *app.Service) error {
	var in struct {
		DryRun bool     `json:"dry_run"`
		IDs    []string `json:"ids"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	cvs, err := a.ConvertToLinks(ctx(r), nil, in.IDs, in.DryRun)
	if err != nil {
		return err
	}
	out := make([]Conversion, len(cvs))
	for i, cv := range cvs {
		out[i] = Conversion{newChannel(cv.Private), newChannel(cv.Shared), newChannel(cv.Link)}
	}
	writeJSON(w, http.StatusOK, map[string]any{"conversions": out})
	return nil
}

func (s *server) changes(w http.ResponseWriter, r *http.Request, a *app.Service) error {
	chs, err := a.Changes(ctx(r))
	if err != nil {
		return err
	}
	out := make([]Change, len(chs))
	for i, c := range chs {
		out[i] = newChange(c)
	}
	writeJSON(w, http.StatusOK, map[string]any{"changes": out})
	return nil
}

func (s *server) ack(w http.ResponseWriter, r *http.Request, a *app.Service) error {
	return s.selectAndDo(w, r, a.Acknowledge)
}

func (s *server) relink(w http.ResponseWriter, r *http.Request, a *app.Service) error {
	return s.selectAndDo(w, r, a.Relink)
}

func (s *server) selectAndDo(w http.ResponseWriter, r *http.Request, fn func(context.Context, app.Selection) ([]domain.Channel, error)) error {
	var in selectInput
	if err := readJSON(r, &in); err != nil {
		return err
	}
	sel, err := in.Select.app()
	if err != nil {
		return err
	}
	cs, err := fn(ctx(r), sel)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, channelsResult{newChannels(cs), []string{}})
	return nil
}

// feed lists the library's channels changed after ?since (RFC 3339; all
// when absent), and the cursor to ask with next time. It's how a local
// copy keeps up with the shared list.
func (s *server) feed(w http.ResponseWriter, r *http.Request, a *app.Service) error {
	var since time.Time
	if v := r.URL.Query().Get("since"); v != "" {
		t, err := time.Parse(time.RFC3339Nano, v)
		if err != nil {
			return badRequest("since must be an RFC 3339 time, as a previous cursor gives it")
		}
		since = t
	}
	cs, cursor, err := a.ChangedSince(ctx(r), since)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"channels": newChannels(cs), "cursor": cursor.UTC().Format(time.RFC3339Nano)})
	return nil
}

// --- files ---

// namedProfile finds a profile by name. Paths aren't accepted over HTTP.
func namedProfile(name string) (profile.Entry, error) {
	if name == "" {
		return profile.Entry{}, badRequest("give a profile name")
	}
	if strings.ContainsAny(name, `/\`) || strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml") {
		return profile.Entry{}, badRequest("profiles are named, not given as files, over HTTP")
	}
	return profile.Find(name)
}

// requestFile returns an upload: the "file" part of a multipart form, or
// the whole body.
func requestFile(r *http.Request) (io.Reader, error) {
	mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mt != "multipart/form-data" {
		return r.Body, nil
	}
	f, _, err := r.FormFile("file")
	if err != nil {
		return nil, badRequest("the form needs a file part named \"file\": %v", err)
	}
	return f, nil
}

type rowError struct {
	Row     int    `json:"row"`
	Message string `json:"message"`
}

func rowErrors(es []importer.RowError) []rowError {
	out := make([]rowError, len(es))
	for i, e := range es {
		out[i] = rowError{e.Row, e.Err.Error()}
	}
	return out
}

func (s *server) importFile(w http.ResponseWriter, r *http.Request, a *app.Service) error {
	parsers := map[string]func(io.Reader) (importer.Batch, error){"csv": importer.ParseGenericCSV, "json": importer.ParseJSON, "chirp": importer.ParseCHIRP}
	format := r.PathValue("format")
	parse, ok := parsers[format]
	if format == "profile" {
		e, err := namedProfile(r.URL.Query().Get("profile"))
		if err != nil {
			return err
		}
		parse, ok = profile.Parser{Profile: e.Profile}.Parse, true
	}
	if !ok {
		return apiError{http.StatusNotFound, "not_found", fmt.Sprintf("unknown import format %q; use csv, json, chirp, or profile", format)}
	}
	q := r.URL.Query()
	var opts importer.Options
	var err error
	if opts.OnDuplicate, err = importer.ParseDuplicatePolicy(cmpOr(q.Get("on_duplicate"), "skip")); err != nil {
		return err
	}
	if opts.Match, err = importer.ParseMatchRule(cmpOr(q.Get("match"), "exact")); err != nil {
		return err
	}
	if v := q.Get("tags"); v != "" {
		opts.Tags = strings.Split(v, ",")
	}
	if opts.Service, err = domain.ParseService(q.Get("service")); err != nil {
		return err
	}
	if q.Has("rx_only") {
		v, err := queryBool(r, "rx_only")
		if err != nil {
			return err
		}
		opts.RXOnly = &v
	}
	if opts.DryRun, err = queryBool(r, "dry_run"); err != nil {
		return err
	}
	skipInvalid, err := queryBool(r, "skip_invalid")
	if err != nil {
		return err
	}
	f, err := requestFile(r)
	if err != nil {
		return err
	}
	b, err := parse(f)
	if err != nil {
		return badRequest("%v", err)
	}
	if len(b.Errors) > 0 && !skipInvalid {
		msgs := make([]string, 0, 3)
		for _, e := range b.Errors[:min(3, len(b.Errors))] {
			msgs = append(msgs, e.Error())
		}
		return badRequest("%s; nothing imported (fix them, or set skip_invalid): %s", plural(len(b.Errors), "invalid row"), strings.Join(msgs, "; "))
	}
	var sum importer.Summary
	var dropped []string
	if a.Library().Shared {
		reason := q.Get("reason")
		if strings.TrimSpace(reason) == "" {
			return badRequest("an import into the shared library needs a reason")
		}
		sum, dropped, err = a.ImportShared(ctx(r), b, opts, reason)
	} else {
		sum, err = a.Import(ctx(r), b, opts)
	}
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"created": sum.Created, "updated": sum.Updated, "skipped": sum.Skipped,
		"invalid": rowErrors(b.Errors), "warnings": rowErrors(sum.Warnings), "dropped_tags": nonNil(dropped), "dry_run": opts.DryRun})
	return nil
}

var exportTypes = map[string]string{"csv": "text/csv", "json": "application/json", "chirp": "text/csv", "profile": "text/csv"}

func (s *server) exportFile(w http.ResponseWriter, r *http.Request, a *app.Service) error {
	format := r.PathValue("format")
	if _, ok := exportTypes[format]; !ok {
		return apiError{http.StatusNotFound, "not_found", fmt.Sprintf("unknown export format %q; use csv, json, chirp, or profile", format)}
	}
	q := r.URL.Query()
	req := app.ExportRequest{Format: format, Sort: q.Get("sort"), Dedupe: q.Get("dedupe")}
	var err error
	if format == "profile" {
		e, err := namedProfile(q.Get("profile"))
		if err != nil {
			return err
		}
		req.Profile = e.Profile.Name
	}
	if text := q.Get("q"); strings.TrimSpace(text) != "" {
		if req.Query, err = query.Parse(text); err != nil {
			return err
		}
	}
	if req.Start, err = queryInt(r, "start"); err != nil {
		return err
	}
	if n, err := queryInt(r, "max_name_len"); err != nil {
		return err
	} else if n != nil {
		req.MaxNameLen = *n
	}
	if req.SkipInvalid, err = queryBool(r, "skip_invalid"); err != nil {
		return err
	}
	var buf bytes.Buffer
	res, err := a.Export(ctx(r), &buf, req)
	if err != nil {
		return err
	}
	exportHeaders(w, exportTypes[format], fmt.Sprintf("%s.%s", a.Library().Name, map[bool]string{true: "json", false: "csv"}[format == "json"]), res.Count, res.Skipped, res.Duplicates)
	_, err = w.Write(buf.Bytes())
	return err
}

// exportHeaders describe a file download: its type and name, the channel
// count, and what was left out or came out the same, which a file has no
// room for.
func exportHeaders(w http.ResponseWriter, contentType, filename string, count int, skipped, duplicates []string) {
	h := w.Header()
	h.Set("Content-Type", contentType)
	h.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filename}))
	h.Set("X-Rflib-Count", fmt.Sprint(count))
	for _, v := range skipped {
		h.Add("X-Rflib-Skipped", v)
	}
	for _, v := range duplicates {
		h.Add("X-Rflib-Duplicate-Name", v)
	}
}

// --- saved searches ---

type searchInput struct {
	Name  *string `json:"name"`
	Query *string `json:"query"`
	Notes *string `json:"notes"`
}

func (s *server) listSearches(w http.ResponseWriter, r *http.Request, a *app.Service) error {
	xs, err := a.ListSearches(ctx(r))
	if err != nil {
		return err
	}
	out := make([]Search, len(xs))
	for i, x := range xs {
		out[i] = newSearch(x)
	}
	writeJSON(w, http.StatusOK, map[string]any{"searches": out})
	return nil
}

func (s *server) searchDetail(w http.ResponseWriter, r *http.Request, a *app.Service, status int, ref string) error {
	d, err := a.SearchDetail(ctx(r), ref)
	if err != nil {
		return err
	}
	o := newSearch(d.SearchSummary)
	o.UsedBy = nonNil(d.Users)
	writeJSON(w, status, o)
	return nil
}

func (s *server) createSearch(w http.ResponseWriter, r *http.Request, a *app.Service) error {
	var in searchInput
	if err := readJSON(r, &in); err != nil {
		return err
	}
	var x domain.SavedSearch
	if in.Name != nil {
		x.Name = *in.Name
	}
	if in.Query != nil {
		x.Query = *in.Query
	}
	if in.Notes != nil {
		x.Notes = *in.Notes
	}
	x, err := a.CreateSearch(ctx(r), x)
	if err != nil {
		return err
	}
	return s.searchDetail(w, r, a, http.StatusCreated, x.ID)
}

func (s *server) getSearch(w http.ResponseWriter, r *http.Request, a *app.Service) error {
	return s.searchDetail(w, r, a, http.StatusOK, r.PathValue("name"))
}

func (s *server) updateSearch(w http.ResponseWriter, r *http.Request, a *app.Service) error {
	var in searchInput
	if err := readJSON(r, &in); err != nil {
		return err
	}
	x, err := a.GetSearch(ctx(r), r.PathValue("name"))
	if err != nil {
		return err
	}
	if in.Name != nil {
		x.Name = *in.Name
	}
	if in.Query != nil {
		x.Query = *in.Query
	}
	if in.Notes != nil {
		x.Notes = *in.Notes
	}
	if x, err = a.UpdateSearch(ctx(r), x); err != nil {
		return err
	}
	return s.searchDetail(w, r, a, http.StatusOK, x.ID)
}

func (s *server) deleteSearch(w http.ResponseWriter, r *http.Request, a *app.Service) error {
	if err := a.DeleteSearch(ctx(r), r.PathValue("name")); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// --- radios ---

type radioInput struct {
	Name         *string `json:"name"`
	Model        *string `json:"model"`
	DefaultPower *string `json:"default_power"`
	Dedupe       *string `json:"dedupe"`
	NameLength   *int    `json:"name_length"`
	Notes        *string `json:"notes"`
}

func (in radioInput) apply(r *domain.Radio) error {
	if in.Name != nil {
		r.Name = *in.Name
	}
	if in.Model != nil {
		m, err := model.Find(*in.Model)
		if err != nil {
			return err
		}
		r.Model = m.Name
	}
	if in.DefaultPower != nil {
		p, err := domain.ParsePowerIntent(*in.DefaultPower)
		if err != nil {
			return err
		}
		r.DefaultPower = p
	}
	if in.Dedupe != nil {
		r.Dedupe = *in.Dedupe
	}
	if in.NameLength != nil {
		r.NameLength = *in.NameLength
	}
	if in.Notes != nil {
		r.Notes = *in.Notes
	}
	return nil
}

func (s *server) radioOut(r *http.Request, a *app.Service, radio domain.Radio) (Radio, error) {
	_, es, err := a.RadioEntries(ctx(r), radio.ID)
	return newRadio(radio, len(es)), err
}

func (s *server) listRadios(w http.ResponseWriter, r *http.Request, a *app.Service) error {
	rs, err := a.ListRadios(ctx(r))
	if err != nil {
		return err
	}
	out := make([]Radio, len(rs))
	for i, radio := range rs {
		if out[i], err = s.radioOut(r, a, radio); err != nil {
			return err
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"radios": out})
	return nil
}

func (s *server) createRadio(w http.ResponseWriter, r *http.Request, a *app.Service) error {
	var in radioInput
	if err := readJSON(r, &in); err != nil {
		return err
	}
	if in.Model == nil {
		return badRequest("a radio needs a model (see /models)")
	}
	var radio domain.Radio
	if err := in.apply(&radio); err != nil {
		return err
	}
	radio, err := a.CreateRadio(ctx(r), radio)
	if err != nil {
		return err
	}
	out, err := s.radioOut(r, a, radio)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, out)
	return nil
}

func (s *server) getRadio(w http.ResponseWriter, r *http.Request, a *app.Service) error {
	radio, err := a.GetRadio(ctx(r), r.PathValue("radio"))
	if err != nil {
		return err
	}
	out, err := s.radioOut(r, a, radio)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

func (s *server) updateRadio(w http.ResponseWriter, r *http.Request, a *app.Service) error {
	var in radioInput
	if err := readJSON(r, &in); err != nil {
		return err
	}
	radio, err := a.GetRadio(ctx(r), r.PathValue("radio"))
	if err != nil {
		return err
	}
	if err := in.apply(&radio); err != nil {
		return err
	}
	if radio, err = a.UpdateRadio(ctx(r), radio); err != nil {
		return err
	}
	out, err := s.radioOut(r, a, radio)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

func (s *server) deleteRadio(w http.ResponseWriter, r *http.Request, a *app.Service) error {
	if err := a.DeleteRadio(ctx(r), r.PathValue("radio")); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *server) memories(w http.ResponseWriter, r *http.Request, a *app.Service) error {
	p, es, err := a.Layout(ctx(r), r.PathValue("radio"))
	if err != nil {
		return err
	}
	ms := newMemories(p, es)
	// A search only filters the rows: memory numbers and warnings are the
	// whole radio's.
	if q := r.URL.Query().Get("q"); strings.TrimSpace(q) != "" {
		cs, err := a.Find(ctx(r), q)
		if err != nil {
			return err
		}
		match := make(map[string]bool, len(cs))
		for _, c := range cs {
			match[c.ID] = true
		}
		ms = slices.DeleteFunc(ms, func(m Memory) bool { return !match[m.Channel.ID] })
	}
	writeJSON(w, http.StatusOK, map[string]any{"memories": ms, "warnings": nonNil(p.Warnings)})
	return nil
}

type addInput struct {
	IDs     []string `json:"ids"`
	Query   string   `json:"query"`
	Sort    string   `json:"sort"`
	AnyBand bool     `json:"any_band"`
	AnyMode bool     `json:"any_mode"`
}

func (s *server) addRadioChannels(w http.ResponseWriter, r *http.Request, a *app.Service) error {
	var in addInput
	if err := readJSON(r, &in); err != nil {
		return err
	}
	req := app.AddRequest{IDs: in.IDs, Sort: in.Sort, AnyBand: in.AnyBand, AnyMode: in.AnyMode}
	if strings.TrimSpace(in.Query) != "" {
		n, err := query.Parse(in.Query)
		if err != nil {
			return err
		}
		req.Query = n
	}
	if len(req.IDs) == 0 && req.Query == nil {
		return badRequest("give channels by ids or query")
	}
	_, res, err := a.AddChannels(ctx(r), r.PathValue("radio"), req)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, newAddResult(res))
	return nil
}

// AddResult is what adding to a radio did: how many channels were new to
// it, the channels that fit it (including any already on it), and those
// skipped as outside its bands or in a mode it lacks.
type AddResult struct {
	Added     int       `json:"added"`
	Channels  []Channel `json:"channels"`
	Skipped   []Channel `json:"skipped"`
	WrongMode []Channel `json:"wrong_mode"`
}

func newAddResult(res app.AddResult) AddResult {
	return AddResult{res.Added, newChannels(res.Add), newChannels(res.Skipped), newChannels(res.WrongMode)}
}

func (s *server) removeRadioChannels(w http.ResponseWriter, r *http.Request, a *app.Service) error {
	var in selectInput
	if err := readJSON(r, &in); err != nil {
		return err
	}
	sel, err := in.Select.app()
	if err != nil {
		return err
	}
	var cs []domain.Channel
	if in.DryRun {
		_, cs, err = a.OnRadio(ctx(r), r.PathValue("radio"), sel)
	} else {
		_, cs, err = a.RemoveChannels(ctx(r), r.PathValue("radio"), sel)
	}
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, channelsResult{newChannels(cs), []string{}})
	return nil
}

// entryInput sets a radio's overrides. A null removes an override; absent
// fields are left alone.
type entryInput struct {
	Name         json.RawMessage `json:"name"`
	Power        json.RawMessage `json:"power"`
	Scan         json.RawMessage `json:"scan"`
	SectionStart json.RawMessage `json:"section_start"`
}

func (in entryInput) edit() (app.EntryEdit, error) {
	var e app.EntryEdit
	if in.Power != nil {
		e.SetPower = true
		if !isNull(in.Power) {
			var v string
			if err := json.Unmarshal(in.Power, &v); err != nil {
				return e, badRequest("power: %v", err)
			}
			p, err := domain.ParsePowerIntent(v)
			if err != nil {
				return e, err
			}
			e.Power = p
		}
	}
	if in.Scan != nil {
		e.SetSkip = true
		if !isNull(in.Scan) {
			var scan bool
			if err := json.Unmarshal(in.Scan, &scan); err != nil {
				return e, badRequest("scan: %v", err)
			}
			skip := !scan
			e.Skip = &skip
		}
	}
	return e, nil
}

func (s *server) setRadioChannel(w http.ResponseWriter, r *http.Request, a *app.Service) error {
	var in entryInput
	if err := readJSON(r, &in); err != nil {
		return err
	}
	edit, err := in.edit()
	if err != nil {
		return err
	}
	radio, e, err := a.RadioEntry(ctx(r), r.PathValue("radio"), r.PathValue("id"))
	if err != nil {
		return err
	}
	if in.Name != nil {
		e.Name = ""
		if !isNull(in.Name) {
			if err := json.Unmarshal(in.Name, &e.Name); err != nil {
				return badRequest("name: %v", err)
			}
		}
	}
	if edit.SetPower {
		e.Power = edit.Power
	}
	if edit.SetSkip {
		e.Skip = edit.Skip
	}
	if in.SectionStart != nil {
		e.SectionStart = nil
		if !isNull(in.SectionStart) {
			var n int
			if err := json.Unmarshal(in.SectionStart, &n); err != nil {
				return badRequest("section_start: %v", err)
			}
			e.SectionStart = &n
		}
	}
	if err := a.SetEntry(ctx(r), radio.ID, e); err != nil {
		return err
	}
	return s.memories(w, r, a)
}

type setInput struct {
	Select Selection       `json:"select"`
	Power  json.RawMessage `json:"power"`
	Scan   json.RawMessage `json:"scan"`
}

func (s *server) setRadioChannels(w http.ResponseWriter, r *http.Request, a *app.Service) error {
	var in setInput
	if err := readJSON(r, &in); err != nil {
		return err
	}
	sel, err := in.Select.app()
	if err != nil {
		return err
	}
	edit, err := entryInput{Power: in.Power, Scan: in.Scan}.edit()
	if err != nil {
		return err
	}
	if !edit.SetPower && !edit.SetSkip {
		return badRequest("nothing to set; give power or scan (name and section_start are set one channel at a time)")
	}
	cs, err := a.SetEntries(ctx(r), r.PathValue("radio"), sel, edit)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, channelsResult{newChannels(cs), []string{}})
	return nil
}

type moveInput struct {
	Before string `json:"before"`
	After  string `json:"after"`
	To     int    `json:"to"`
	Top    bool   `json:"top"`
	Bottom bool   `json:"bottom"`
}

func (s *server) moveRadioChannel(w http.ResponseWriter, r *http.Request, a *app.Service) error {
	var in moveInput
	if err := readJSON(r, &in); err != nil {
		return err
	}
	given := 0
	for _, set := range []bool{in.Before != "", in.After != "", in.To != 0, in.Top, in.Bottom} {
		if set {
			given++
		}
	}
	if given != 1 {
		return badRequest("give one of before, after, to, top, or bottom")
	}
	if err := a.MoveChannel(ctx(r), r.PathValue("radio"), r.PathValue("id"), app.Move(in)); err != nil {
		return err
	}
	return s.memories(w, r, a)
}

func (s *server) listSources(w http.ResponseWriter, r *http.Request, a *app.Service) error {
	_, srcs, es, err := a.Sources(ctx(r), r.PathValue("radio"))
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"sources": newSources(srcs, es)})
	return nil
}

type sourceInput struct {
	Query        *string         `json:"query"`
	Plan         *string         `json:"plan"`
	Sort         *string         `json:"sort"`
	SectionStart json.RawMessage `json:"section_start"`
	KeepSorted   *bool           `json:"keep_sorted"`
}

func (in sourceInput) apply(src *domain.RadioSource) error {
	if in.Query != nil {
		src.Query = *in.Query
	}
	if in.Plan != nil {
		if src.ID != "" && *in.Plan != src.PlanID && *in.Plan != src.PlanName {
			return badRequest("a source's plan can't change; add another source")
		}
		src.PlanID = *in.Plan
	}
	if in.Sort != nil {
		src.Sort = *in.Sort
	}
	if in.KeepSorted != nil {
		src.KeepSorted = *in.KeepSorted
	}
	if in.SectionStart != nil {
		src.SectionStart = nil
		if !isNull(in.SectionStart) {
			var n int
			if err := json.Unmarshal(in.SectionStart, &n); err != nil {
				return badRequest("section_start: %v", err)
			}
			src.SectionStart = &n
		}
	}
	return nil
}

func (s *server) addSource(w http.ResponseWriter, r *http.Request, a *app.Service) error {
	var in sourceInput
	if err := readJSON(r, &in); err != nil {
		return err
	}
	src := domain.RadioSource{Sort: "name"}
	if err := in.apply(&src); err != nil {
		return err
	}
	if _, _, err := a.AddSource(ctx(r), r.PathValue("radio"), src); err != nil {
		return err
	}
	_, srcs, es, err := a.Sources(ctx(r), r.PathValue("radio"))
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, map[string]any{"sources": newSources(srcs, es)})
	return nil
}

// previewSource reports what a new source would add, without adding it.
func (s *server) previewSource(w http.ResponseWriter, r *http.Request, a *app.Service) error {
	var in sourceInput
	if err := readJSON(r, &in); err != nil {
		return err
	}
	src := domain.RadioSource{Sort: "name"}
	if err := in.apply(&src); err != nil {
		return err
	}
	ss, err := a.PreviewSource(ctx(r), r.PathValue("radio"), src, app.SyncOptions{})
	if err != nil {
		return err
	}
	ss.Added = len(ss.Add)
	writeJSON(w, http.StatusOK, newAddResult(ss.AddResult))
	return nil
}

func (s *server) updateSource(w http.ResponseWriter, r *http.Request, a *app.Service) error {
	n, err := pathInt(r, "n")
	if err != nil {
		return err
	}
	var in sourceInput
	if err := readJSON(r, &in); err != nil {
		return err
	}
	_, src, err := a.Source(ctx(r), r.PathValue("radio"), n)
	if err != nil {
		return err
	}
	if err := in.apply(&src); err != nil {
		return err
	}
	if err := a.UpdateSource(ctx(r), src); err != nil {
		return err
	}
	return s.listSources(w, r, a)
}

func (s *server) removeSource(w http.ResponseWriter, r *http.Request, a *app.Service) error {
	n, err := pathInt(r, "n")
	if err != nil {
		return err
	}
	with, err := queryBool(r, "with_channels")
	if err != nil {
		return err
	}
	radio, err := a.GetRadio(ctx(r), r.PathValue("radio"))
	if err != nil {
		return err
	}
	removed, err := a.RemoveSource(ctx(r), radio.ID, n, with)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]int{"channels_removed": removed})
	return nil
}

func (s *server) moveSource(w http.ResponseWriter, r *http.Request, a *app.Service) error {
	n, err := pathInt(r, "n")
	if err != nil {
		return err
	}
	var in struct {
		To int `json:"to"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	if in.To < 1 {
		return badRequest("to counts from 1")
	}
	if err := a.MoveSource(ctx(r), r.PathValue("radio"), n, in.To); err != nil {
		return err
	}
	return s.listSources(w, r, a)
}

type SourceSync struct {
	Source Source `json:"source"`
	AddResult
	Resorted int       `json:"resorted"`
	Removed  []Channel `json:"removed"`
	Linked   int       `json:"linked"`
	Note     string    `json:"note"`
}

func (s *server) syncRadio(w http.ResponseWriter, r *http.Request, a *app.Service) error {
	var in struct {
		DryRun  bool `json:"dry_run"`
		Resort  bool `json:"resort"`
		AnyBand bool `json:"any_band"`
		AnyMode bool `json:"any_mode"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	res, err := a.Sync(ctx(r), r.PathValue("radio"), app.SyncOptions{DryRun: in.DryRun, Resort: in.Resort, AnyBand: in.AnyBand, AnyMode: in.AnyMode})
	if err != nil {
		return err
	}
	out := make([]SourceSync, len(res.Sources))
	for i, ss := range res.Sources {
		added := ss.AddResult
		if in.DryRun {
			added.Added = len(added.Add)
		}
		out[i] = SourceSync{newSources([]domain.RadioSource{ss.Source}, nil)[0], newAddResult(added), ss.Resorted, newChannels(ss.Removed), ss.Linked, ss.Note}
	}
	var layoutErr *string
	if res.LayoutErr != nil {
		layoutErr = optional(res.LayoutErr.Error())
	}
	writeJSON(w, http.StatusOK, map[string]any{"sources": out, "added": res.Added(), "resorted": res.Resorted(), "dry_run": in.DryRun, "layout_error": layoutErr})
	return nil
}

func (s *server) exportRadio(w http.ResponseWriter, r *http.Request, a *app.Service) error {
	skipInvalid, err := queryBool(r, "skip_invalid")
	if err != nil {
		return err
	}
	p, _, err := a.Layout(ctx(r), r.PathValue("radio"))
	if err != nil {
		return err
	}
	var buf, skipped bytes.Buffer
	n, err := program.Write(&buf, p, skipInvalid, &skipped)
	if err != nil {
		return err
	}
	name := strings.NewReplacer("/", "-", " ", "-").Replace(p.Radio.Name) + ".csv"
	var left []string
	for _, line := range strings.Split(strings.TrimSpace(skipped.String()), "\n") {
		if line != "" {
			left = append(left, line)
		}
	}
	exportHeaders(w, "text/csv", name, n, left, nil)
	for _, warning := range p.Warnings {
		w.Header().Add("X-Rflib-Warning", warning)
	}
	_, err = w.Write(buf.Bytes())
	return err
}

// --- catalog ---

func (s *server) listModels(w http.ResponseWriter, _ *http.Request) error {
	all, err := model.All()
	if err != nil {
		return err
	}
	out := make([]Model, len(all))
	for i, m := range all {
		out[i] = newModel(m)
	}
	writeJSON(w, http.StatusOK, map[string]any{"models": out})
	return nil
}

func (s *server) getModel(w http.ResponseWriter, r *http.Request) error {
	m, err := model.Find(r.PathValue("name"))
	if err != nil {
		return apiError{http.StatusNotFound, "not_found", err.Error()}
	}
	writeJSON(w, http.StatusOK, newModel(m))
	return nil
}

func (s *server) listProfiles(w http.ResponseWriter, _ *http.Request) error {
	entries, err := profile.Available()
	if err != nil {
		return err
	}
	type out struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		BuiltIn     bool   `json:"built_in"`
	}
	list := make([]out, len(entries))
	for i, e := range entries {
		list[i] = out{e.Profile.Name, e.Profile.Description, e.Origin == "built-in"}
	}
	writeJSON(w, http.StatusOK, map[string]any{"profiles": list})
	return nil
}

type seedOut struct {
	ID          string    `json:"id"`
	Description string    `json:"description"`
	Count       int       `json:"count"`
	Installed   bool      `json:"installed"`
	Pending     int       `json:"pending"`
	Channels    []Channel `json:"channels,omitempty"`
}

// seedChange is one change a list's update makes to the shared list.
type seedChange struct {
	Kind    string   `json:"kind"`
	Name    string   `json:"name"`
	Fields  []string `json:"fields"`
	Kept    []string `json:"kept"`
	Channel Channel  `json:"channel"`
}

// sharedService opens the shared list's operations.
func (s *server) sharedService(r *http.Request) (*app.Service, error) {
	return app.Open(ctx(r), s.st, domain.SharedLibrary)
}

func seedInfo(r *http.Request, sh *app.Service, l seed.List, withChannels bool) (seedOut, error) {
	st, err := sh.SeedStatus(ctx(r), l)
	if err != nil {
		return seedOut{}, err
	}
	b, err := l.Batch()
	if err != nil {
		return seedOut{}, err
	}
	o := seedOut{ID: l.ID, Description: l.Description, Count: len(b.Records), Installed: st.Installed, Pending: len(st.Changes)}
	if withChannels {
		o.Channels = []Channel{}
		for _, rec := range b.Records {
			o.Channels = append(o.Channels, newChannel(rec.Channel))
		}
	}
	return o, nil
}

func (s *server) listSeeds(w http.ResponseWriter, r *http.Request) error {
	sh, err := s.sharedService(r)
	if err != nil {
		return err
	}
	var out []seedOut
	for _, l := range seed.Lists() {
		o, err := seedInfo(r, sh, l, false)
		if err != nil {
			return err
		}
		out = append(out, o)
	}
	writeJSON(w, http.StatusOK, map[string]any{"seeds": nonNil(out)})
	return nil
}

func (s *server) getSeed(w http.ResponseWriter, r *http.Request) error {
	l, err := seed.Get(r.PathValue("seed"))
	if err != nil {
		return apiError{http.StatusNotFound, "not_found", err.Error()}
	}
	sh, err := s.sharedService(r)
	if err != nil {
		return err
	}
	o, err := seedInfo(r, sh, l, true)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, o)
	return nil
}

// updateSeed brings the shared list up to date with a built-in list, or
// with dry_run reports what that would change.
func (s *server) updateSeed(w http.ResponseWriter, r *http.Request) error {
	l, err := seed.Get(r.PathValue("seed"))
	if err != nil {
		return apiError{http.StatusNotFound, "not_found", err.Error()}
	}
	var in struct {
		DryRun bool `json:"dry_run"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	sh, err := s.sharedService(r)
	if err != nil {
		return err
	}
	st, err := sh.UpdateSeed(ctx(r), l, in.DryRun)
	if err != nil {
		return err
	}
	changes := make([]seedChange, len(st.Changes))
	for i, ch := range st.Changes {
		changes[i] = seedChange{ch.Kind, ch.Name, nonNil(ch.Fields), nonNil(ch.Kept), newChannel(ch.Channel)}
	}
	writeJSON(w, http.StatusOK, map[string]any{"installed": st.Installed, "dry_run": in.DryRun, "changes": changes})
	return nil
}

func (s *server) listTags(w http.ResponseWriter, _ *http.Request) error {
	type tag struct {
		Name    string `json:"name"`
		Meaning string `json:"meaning"`
	}
	out := make([]tag, len(domain.CuratedTags))
	for i, t := range domain.CuratedTags {
		out[i] = tag{t.Name, t.Meaning}
	}
	writeJSON(w, http.StatusOK, map[string]any{"tags": out})
	return nil
}
