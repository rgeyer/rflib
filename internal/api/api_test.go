package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/legacy"
	"github.com/ryan/rflib/internal/store/sqlite"
)

// client calls the API and holds every request and response to the spec.
type client struct {
	t      *testing.T
	srv    *httptest.Server
	router routers.Router
	token  string
}

func newClient(t *testing.T, token string) *client {
	t.Helper()
	d, err := sqlite.Open(filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := d.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	doc, err := openapi3.NewLoader().LoadFromData(Spec)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(New(d, Options{Token: token}))
	t.Cleanup(srv.Close)
	doc.Servers = openapi3.Servers{{URL: srv.URL + Prefix}}
	if err := doc.Validate(context.Background()); err != nil {
		t.Fatalf("the spec is invalid: %v", err)
	}
	router, err := legacy.NewRouter(doc)
	if err != nil {
		t.Fatal(err)
	}
	return &client{t: t, srv: srv, router: router, token: token}
}

// do sends a request with a JSON (or raw string) body, checks it and the
// response against the spec, and decodes a JSON response into out.
func (c *client) do(method, path string, body any, want int, out any) http.Header {
	c.t.Helper()
	var data []byte
	contentType := "application/json"
	switch b := body.(type) {
	case nil:
	case string:
		data, contentType = []byte(b), "text/csv"
	default:
		var err error
		if data, err = json.Marshal(b); err != nil {
			c.t.Fatal(err)
		}
	}
	req, err := http.NewRequest(method, c.srv.URL+Prefix+path, bytes.NewReader(data))
	if err != nil {
		c.t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", contentType)
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	route, params, err := c.router.FindRoute(req)
	if err != nil {
		c.t.Fatalf("%s %s isn't in the spec: %v", method, path, err)
	}
	opts := &openapi3filter.Options{AuthenticationFunc: openapi3filter.NoopAuthenticationFunc, IncludeResponseStatus: true}
	in := &openapi3filter.RequestValidationInput{Request: req, PathParams: params, Route: route, Options: opts}
	if err := openapi3filter.ValidateRequest(context.Background(), in); err != nil {
		c.t.Fatalf("%s %s: the request doesn't match the spec: %v", method, path, err)
	}
	req.Body = io.NopCloser(bytes.NewReader(data))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	got, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != want {
		c.t.Fatalf("%s %s = %d, want %d: %s", method, path, resp.StatusCode, want, got)
	}
	resIn := &openapi3filter.ResponseValidationInput{RequestValidationInput: in, Status: resp.StatusCode, Header: resp.Header, Body: io.NopCloser(bytes.NewReader(got)), Options: opts}
	if err := openapi3filter.ValidateResponse(context.Background(), resIn); err != nil {
		c.t.Fatalf("%s %s: the response doesn't match the spec: %v\n%s", method, path, err, got)
	}
	if out != nil {
		if s, ok := out.(*string); ok {
			*s = string(got)
		} else if err := json.Unmarshal(got, out); err != nil {
			c.t.Fatalf("%s %s: %v\n%s", method, path, err, got)
		}
	}
	return resp.Header
}

// errCode is the code of an error response.
func (c *client) errCode(method, path string, body any, want int) string {
	c.t.Helper()
	var e errorBody
	c.do(method, path, body, want, &e)
	return e.Error.Code
}

// Every route is in the spec, and every operation in the spec is routed.
func TestRoutesMatchTheSpec(t *testing.T) {
	doc, err := openapi3.NewLoader().LoadFromData(Spec)
	if err != nil {
		t.Fatal(err)
	}
	var inSpec, routed []string
	for path, item := range doc.Paths.Map() {
		for method := range item.Operations() {
			inSpec = append(inSpec, method+" "+path)
		}
	}
	for _, r := range Routes() {
		routed = append(routed, r.Method+" "+r.Path)
	}
	sort.Strings(inSpec)
	sort.Strings(routed)
	if !slices.Equal(inSpec, routed) {
		t.Errorf("spec and routes differ:\nspec:   %v\nroutes: %v", inSpec, routed)
	}
}

func TestAuthAndErrors(t *testing.T) {
	c := newClient(t, "secret")
	c.do("GET", "/health", nil, 200, nil)
	var spec string
	c.do("GET", "/openapi.yaml", nil, 200, &spec)
	if !strings.HasPrefix(spec, "openapi: ") {
		t.Errorf("spec = %.40q", spec)
	}
	c.token = "wrong"
	if code := c.errCode("GET", "/libraries", nil, 401); code != "unauthorized" {
		t.Errorf("bad token: %s", code)
	}
	c.token = "secret"
	if code := c.errCode("GET", "/libraries/nope/channels", nil, 404); code != "not_found" {
		t.Errorf("missing library: %s", code)
	}
	if code := c.errCode("POST", "/libraries/default/channels", map[string]any{"name": "x", "rx_hz": 146520000, "mode": "DMR"}, 400); code != "invalid" {
		t.Errorf("bad mode: %s", code)
	}
	if code := c.errCode("GET", "/libraries/default/channels?q=frq:1", nil, 400); code != "invalid" {
		t.Errorf("bad query: %s", code)
	}
	c.do("POST", "/libraries", map[string]any{"name": "club"}, 201, nil)
	if code := c.errCode("POST", "/libraries", map[string]any{"name": "club"}, 409); code != "conflict" {
		t.Errorf("duplicate library: %s", code)
	}
	if code := c.errCode("POST", "/libraries/shared/channels", map[string]any{"name": "x", "rx_hz": 146520000}, 403); code != "read_only" {
		t.Errorf("write to shared: %s", code)
	}
	if code := c.errCode("DELETE", "/libraries/shared", nil, 400); code != "invalid" {
		t.Errorf("delete shared: %s", code)
	}
}

func TestChannelsRadiosAndShared(t *testing.T) {
	c := newClient(t, "")
	L := "/libraries/default"

	// Channels: create, patch, clear, search.
	var made channelResult
	c.do("POST", L+"/channels", map[string]any{"name": "Marian", "rx_hz": 147300000, "tx_hz": 147900000, "tx_tone": "ctcss:131.8",
		"tags": []string{"ham"}, "location": map[string]any{"latitude": 34.95, "longitude": -120.43, "radius_km": 40}}, 201, &made)
	id := made.Channel.ID
	if made.Channel.Location == nil || *made.Channel.Location.RadiusKM != 40 || made.Channel.Status != "active" {
		t.Fatalf("created = %+v", made.Channel)
	}
	var patched channelResult
	c.do("PATCH", L+"/channels/"+id[:8], map[string]any{"power": "low", "tx_hz": nil, "location": nil}, 200, &patched)
	if patched.Channel.TxHz != nil || patched.Channel.Location != nil || patched.Channel.Power != "low" || patched.Channel.Name != "Marian" {
		t.Errorf("patched = %+v", patched.Channel)
	}
	if code := c.errCode("PATCH", L+"/channels/"+id, map[string]any{"frequency": 1}, 400); code != "invalid" {
		t.Errorf("unknown field: %s", code)
	}
	var list struct {
		Channels []Channel `json:"channels"`
		Total    int       `json:"total"`
	}
	c.do("GET", L+"/channels?q=tag:ham&limit=0", nil, 200, &list)
	if list.Total != 1 || len(list.Channels) != 0 {
		t.Errorf("list = %+v", list)
	}

	// Built-in lists and links.
	var installed struct {
		Linked []Channel `json:"linked"`
	}
	c.do("POST", L+"/seeds/us-noaa/install", map[string]any{}, 200, &installed)
	if len(installed.Linked) != 7 || installed.Linked[0].Link == nil {
		t.Fatalf("install = %+v", installed)
	}
	wx := installed.Linked[0]
	if code := c.errCode("PATCH", L+"/channels/"+wx.ID, map[string]any{"rx_hz": 162400000}, 400); code != "invalid" {
		t.Errorf("edit a shared field: %s", code)
	}
	c.do("GET", L+"/channels?q="+urlQuery("in:shared band:noaa"), nil, 200, &list)
	if list.Total != 7 {
		t.Errorf("in:shared = %d", list.Total)
	}

	// Bulk changes.
	var bulk channelsResult
	c.do("POST", L+"/channels/bulk-update", map[string]any{"select": map[string]any{"query": "band:noaa"}, "edit": map[string]any{"tags": map[string]any{"add": []string{"wx"}}, "skip": true}}, 200, &bulk)
	if len(bulk.Channels) != 7 || !bulk.Channels[0].Skip || !slices.Contains(bulk.Channels[0].Tags, "wx") {
		t.Errorf("bulk update = %+v", bulk.Channels[0])
	}

	// A radio, its channels, overrides, sources, sync, and export.
	var radio Radio
	c.do("POST", L+"/radios", map[string]any{"name": "HT", "model": "baofeng-uv5rm"}, 201, &radio)
	var added AddResult
	c.do("POST", L+"/radios/HT/channels", map[string]any{"query": "band:noaa", "sort": "freq"}, 200, &added)
	if added.Added != 7 {
		t.Errorf("added = %+v", added)
	}
	var mems struct {
		Memories []Memory `json:"memories"`
	}
	c.do("PATCH", L+"/radios/HT/channels/"+wx.ID, map[string]any{"name": "WX", "section_start": 10}, 200, &mems)
	c.do("POST", L+"/radios/HT/channels/"+wx.ID+"/move", map[string]any{"top": true}, 200, &mems)
	if mems.Memories[0].Name != "WX" || mems.Memories[0].Memory != 10 || !mems.Memories[0].Overrides.Name {
		t.Errorf("memories = %+v", mems.Memories[0])
	}
	// A search filters the rows but keeps the whole radio's numbering.
	c.do("GET", L+"/radios/HT/memories", nil, 200, &mems)
	last := mems.Memories[len(mems.Memories)-1]
	c.do("GET", L+"/radios/HT/memories?q="+urlQuery("id:"+last.Channel.ID), nil, 200, &mems)
	if len(mems.Memories) != 1 || mems.Memories[0].Memory != last.Memory {
		t.Errorf("searched memories = %+v, want memory %d", mems.Memories, last.Memory)
	}
	c.do("POST", L+"/radios/HT/sources", map[string]any{"query": "tag:ham", "section_start": 50}, 201, nil)
	var sync struct {
		Added int `json:"added"`
	}
	c.do("POST", L+"/radios/HT/sync", map[string]any{}, 200, &sync)
	if sync.Added != 1 {
		t.Errorf("sync = %+v", sync)
	}
	var csv string
	h := c.do("GET", L+"/radios/HT/export", nil, 200, &csv)
	if !strings.HasPrefix(csv, "Location,Name,") || h.Get("X-Rflib-Count") != "8" {
		t.Errorf("export = %s %v", h.Get("X-Rflib-Count"), csv[:min(40, len(csv))])
	}
	c.do("GET", L+"/exports/chirp?q=tag:ham&max_name_len=6", nil, 200, &csv)
	if !strings.Contains(csv, ",Marian,") {
		t.Errorf("chirp export = %s", csv)
	}

	// A maintainer takes a shared channel off the air; it shows in changes.
	var sharedCh Channel
	c.do("POST", "/libraries/shared/channels/"+wx.Link.SharedID+"/status", map[string]any{"status": "off-air", "back_on": "2026-12-01", "reason": "transmitter work"}, 200, &sharedCh)
	if sharedCh.Status != "off-air" || sharedCh.Revision != 2 {
		t.Errorf("status = %+v", sharedCh)
	}
	var changes struct {
		Changes []Change `json:"changes"`
	}
	c.do("GET", L+"/shared/changes", nil, 200, &changes)
	if len(changes.Changes) != 1 || !slices.Contains(changes.Changes[0].Radios, "HT") {
		t.Errorf("changes = %+v", changes)
	}
	var history struct {
		Revisions []Revision `json:"revisions"`
	}
	c.do("GET", L+"/channels/"+wx.ID+"/history", nil, 200, &history)
	if len(history.Revisions) != 2 || history.Revisions[1].Reason != "transmitter work" {
		t.Errorf("history = %+v", history)
	}
	var feed struct {
		Channels []Channel `json:"channels"`
		Cursor   string    `json:"cursor"`
	}
	c.do("GET", "/libraries/shared/feed", nil, 200, &feed)
	all := len(feed.Channels)
	c.do("GET", "/libraries/shared/feed?since="+urlQuery(feed.Cursor), nil, 200, &feed)
	if all != 7 || len(feed.Channels) != 0 {
		t.Errorf("feed = %d, then %d", all, len(feed.Channels))
	}

	// Imports and searches.
	var imported struct {
		Created int `json:"created"`
	}
	c.do("POST", L+"/imports/csv?tags=imported", "name,receive_mhz\nCalling,146.520\n", 200, &imported)
	if imported.Created != 1 {
		t.Errorf("import = %+v", imported)
	}
	var x Search
	c.do("POST", L+"/searches", map[string]any{"name": "wx", "query": "band:noaa"}, 201, &x)
	if x.Matches == nil || *x.Matches != 7 {
		t.Errorf("search = %+v", x)
	}
	c.do("DELETE", L+"/searches/wx", nil, 204, nil)

	// The catalog.
	var models struct {
		Models []Model `json:"models"`
	}
	c.do("GET", "/models", nil, 200, &models)
	c.do("GET", "/models/baofeng-uv5rm", nil, 200, nil)
	c.do("GET", "/profiles", nil, 200, nil)
	c.do("GET", "/seeds", nil, 200, nil)
	c.do("GET", "/seeds/us-gmrs", nil, 200, nil)
	c.do("GET", "/tags", nil, 200, nil)
	if code := c.errCode("GET", "/models/nope", nil, 404); code != "not_found" {
		t.Errorf("missing model: %s", code)
	}
	if len(models.Models) == 0 {
		t.Error("no models")
	}

	// Copy, delete, and cleanup.
	var copies channelsResult
	c.do("POST", L+"/channels/copy", map[string]any{"select": map[string]any{"ids": []string{wx.ID}}}, 200, &copies)
	if len(copies.Channels) != 1 || copies.Channels[0].Link != nil {
		t.Errorf("copy = %+v", copies)
	}
	c.do("DELETE", L+"/channels/"+id, nil, 204, nil)
	c.do("DELETE", L+"/radios/HT", nil, 204, nil)
}

func urlQuery(s string) string {
	r := strings.NewReplacer(" ", "%20", ":", "%3A", "+", "%2B")
	return r.Replace(s)
}
