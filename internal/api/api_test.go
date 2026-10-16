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
	"github.com/ryan/rflib/internal/app"
	"github.com/ryan/rflib/internal/seed"
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
// seedAndLink puts built-in lists in the shared list and returns the
// default library's views of their channels, in list order.
func (c *client) seedAndLink(lists ...string) []Channel {
	c.t.Helper()
	var out []Channel
	for _, id := range lists {
		c.do("POST", "/seeds/"+id+"/update", map[string]any{}, 200, nil)
		l, err := seed.Get(id)
		if err != nil {
			c.t.Fatal(err)
		}
		b, err := l.Batch()
		if err != nil {
			c.t.Fatal(err)
		}
		for _, r := range b.Records {
			var v Channel
			c.do("GET", "/libraries/default/channels/"+app.SeedID(id, r.Channel.Name), nil, 200, &v)
			out = append(out, v)
		}
	}
	return out
}

// The built-in lists' status, and an update that installs one.
func TestSeedStatusAndUpdate(t *testing.T) {
	c := newClient(t, "")
	var list struct {
		Seeds []struct {
			ID        string `json:"id"`
			Installed bool   `json:"installed"`
			Pending   int    `json:"pending"`
		} `json:"seeds"`
	}
	c.do("GET", "/seeds", nil, 200, &list)
	if len(list.Seeds) == 0 || list.Seeds[0].Installed || list.Seeds[0].Pending == 0 {
		t.Fatalf("seeds = %+v", list.Seeds)
	}
	var up struct {
		Installed bool `json:"installed"`
		Changes   []struct {
			Kind string `json:"kind"`
			Name string `json:"name"`
		} `json:"changes"`
	}
	c.do("POST", "/seeds/us-noaa/update", map[string]any{"dry_run": true}, 200, &up)
	if up.Installed || len(up.Changes) != 7 || up.Changes[0].Kind != "add" || up.Changes[0].Name != "NOAA WX1" {
		t.Fatalf("dry run = %+v", up)
	}
	c.do("POST", "/seeds/us-noaa/update", map[string]any{}, 200, &up)
	c.do("POST", "/seeds/us-noaa/update", map[string]any{"dry_run": true}, 200, &up)
	if !up.Installed || len(up.Changes) != 0 {
		t.Errorf("after the update = %+v", up)
	}
	if code := c.errCode("POST", "/seeds/us-nope/update", map[string]any{}, 404); code != "not_found" {
		t.Errorf("unknown list: %s", code)
	}
}

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
	if code := c.errCode("POST", "/libraries/shared/channels", map[string]any{"name": "x", "rx_hz": 146520000}, 403); code != "read_only" {
		t.Errorf("write to shared: %s", code)
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
	linked := c.seedAndLink("us-noaa")
	if len(linked) != 7 || linked[0].Link == nil {
		t.Fatalf("seed and link = %+v", linked)
	}
	wx := linked[0]
	if code := c.errCode("PATCH", L+"/channels/"+wx.ID, map[string]any{"rx_hz": 162400000}, 400); code != "invalid" {
		t.Errorf("edit a shared field: %s", code)
	}
	c.do("GET", L+"/channels?q="+urlQuery("in:shared band:noaa"), nil, 200, &list)
	if list.Total != 7 {
		t.Errorf("in:shared = %d", list.Total)
	}

	// Bulk changes.
	var bulk channelsResult
	// Tagging shared channels annotates them, which puts them in the
	// library; their skip is set per radio.
	var wxIDs []string
	for _, v := range linked {
		wxIDs = append(wxIDs, v.ID)
	}
	if code := c.errCode("POST", L+"/channels/bulk-update", map[string]any{"select": map[string]any{"ids": wxIDs}, "edit": map[string]any{"skip": true}}, 400); code != "invalid" {
		t.Errorf("skip on shared channels: %s", code)
	}
	c.do("POST", L+"/channels/bulk-update", map[string]any{"select": map[string]any{"ids": wxIDs}, "edit": map[string]any{"tags": map[string]any{"add": []string{"wx"}}}}, 200, &bulk)
	if len(bulk.Channels) != 7 || bulk.Channels[0].ID != wx.ID || !slices.Contains(bulk.Channels[0].Tags, "wx") {
		t.Errorf("bulk update = %+v", bulk.Channels[0])
	}
	c.do("GET", L+"/channels?q="+urlQuery("band:noaa"), nil, 200, &list)
	if list.Total != 7 {
		t.Errorf("annotated in the library = %d", list.Total)
	}

	// A radio, its channels, overrides, sources, sync, and export.
	var radio Radio
	c.do("POST", L+"/radios", map[string]any{"name": "HT", "model": "baofeng-uv5rm"}, 201, &radio)
	var generic Radio
	c.do("POST", L+"/radios", map[string]any{"name": "Spare", "model": "chirp-generic", "name_length": 6}, 201, &generic)
	if generic.NameLength != 6 {
		t.Errorf("generic radio = %+v", generic)
	}
	c.do("DELETE", L+"/radios/Spare", nil, 204, nil)
	var added AddResult
	c.do("POST", L+"/radios/HT/channels", map[string]any{"query": "band:noaa", "sort": "freq"}, 200, &added)
	if added.Added != 7 {
		t.Errorf("added = %+v", added)
	}
	var mems struct {
		Memories []Memory `json:"memories"`
	}
	c.do("PATCH", L+"/radios/HT/channels/"+wx.ID, map[string]any{"name": "WX"}, 200, &mems)
	// A section of its own, starting at 10.
	var secs struct {
		Sections []Section `json:"sections"`
	}
	c.do("POST", L+"/radios/HT/sections", map[string]any{"name": "Weather", "start": 10, "size": 3}, 201, &secs)
	if len(secs.Sections) != 2 || secs.Sections[0].Name != "Weather" || secs.Sections[0].First != 10 || secs.Sections[0].Last != 9 || !secs.Sections[1].Default {
		t.Errorf("sections = %+v", secs.Sections)
	}
	c.do("POST", L+"/radios/HT/channels/"+wx.ID+"/move", map[string]any{"section": "weather"}, 200, &mems)
	if mems.Memories[0].Name != "WX" || *mems.Memories[0].Memory != 10 || !mems.Memories[0].Overrides.Name || mems.Memories[0].Section != secs.Sections[0].ID {
		t.Errorf("memories = %+v", mems.Memories[0])
	}
	if *mems.Memories[1].Memory != 13 {
		t.Errorf("after the section's size, %+v", mems.Memories[1])
	}
	c.do("PATCH", L+"/radios/HT/sections/Weather", map[string]any{"size": nil, "sort": "freq"}, 200, &secs)
	if secs.Sections[0].Size != nil || secs.Sections[0].Sort != "freq" {
		t.Errorf("updated section = %+v", secs.Sections[0])
	}
	if code := c.errCode("DELETE", L+"/radios/HT/sections/Other", nil, 400); code != "invalid" {
		t.Errorf("delete the default: %s", code)
	}
	c.do("POST", L+"/radios/HT/sections/Other/move", map[string]any{"to": 1}, 200, &secs)
	if secs.Sections[0].Name != "Other" {
		t.Errorf("moved = %+v", secs.Sections)
	}
	c.do("DELETE", L+"/radios/HT/sections/Weather", nil, 200, &secs)
	if len(secs.Sections) != 1 {
		t.Errorf("after delete = %+v", secs.Sections)
	}
	// A search filters the rows but keeps the whole radio's numbering.
	c.do("GET", L+"/radios/HT/memories", nil, 200, &mems)
	last := mems.Memories[len(mems.Memories)-1]
	c.do("GET", L+"/radios/HT/memories?q="+urlQuery("id:"+last.Channel.ID), nil, 200, &mems)
	if len(mems.Memories) != 1 || *mems.Memories[0].Memory != *last.Memory {
		t.Errorf("searched memories = %+v, want memory %d", mems.Memories, *last.Memory)
	}
	c.do("POST", L+"/radios/HT/sections", map[string]any{"name": "Ham", "start": 50}, 201, nil)
	var srcs struct {
		Sources []Source `json:"sources"`
	}
	c.do("POST", L+"/radios/HT/sources", map[string]any{"query": "tag:ham", "section": "Ham"}, 201, &srcs)
	if srcs.Sources[0].Section != "Ham" || srcs.Sources[0].PlanNumbers != "compact" {
		t.Errorf("source = %+v", srcs.Sources[0])
	}
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

func TestOrganizationsSystemsAndPlans(t *testing.T) {
	c := newClient(t, "")
	L := "/libraries/default"
	var link Channel
	for _, v := range c.seedAndLink("us-ham-calling") {
		if v.Name == "2m FM Call" {
			link = v
		}
	}
	shared := link.Link.SharedID

	if code := c.errCode("POST", "/organizations", map[string]any{"name": "SBARC", "reason": ""}, 400); code != "invalid" {
		t.Errorf("no reason: %s", code)
	}
	var org Organization
	c.do("POST", "/organizations", map[string]any{"name": "SBARC", "callsign": "w6sba", "reason": "pilot"}, 201, &org)
	if org.Kind != "club" || org.Callsign != "W6SBA" {
		t.Errorf("org = %+v", org)
	}
	// The shared channel gets an operator and a node; the link shows them.
	c.do("PATCH", "/libraries/shared/channels/"+shared, map[string]any{"organizations": []map[string]any{{"org_id": "SBARC"}}, "networks": []string{"allstar:43761"}, "reason": "node"}, 200, nil)
	var got Channel
	c.do("GET", L+"/channels/"+link.ID, nil, 200, &got)
	if len(got.Orgs) != 1 || got.Orgs[0].Name != "SBARC" || got.Orgs[0].Role != "operator" || strings.Join(got.Networks, ",") != "allstar:43761" {
		t.Errorf("link = %+v", got)
	}

	var sys struct {
		System   System    `json:"system"`
		Channels []Channel `json:"channels"`
	}
	c.do("POST", "/systems", map[string]any{"name": "SBARC Hub", "link": "allstar", "networks": []string{"allstar:43763"},
		"organizations": []map[string]any{{"org_id": org.ID}}, "members": []map[string]any{{"channel_id": shared[:8], "linking": "on-demand"}}, "reason": "pilot"}, 201, &sys)
	if len(sys.Channels) != 1 || sys.System.Members[0].Linking != "on-demand" || sys.System.Members[0].ChannelID != shared {
		t.Errorf("system = %+v", sys)
	}
	var list struct {
		Channels []Channel `json:"channels"`
		Total    int       `json:"total"`
	}
	c.do("GET", L+"/channels?q="+urlQuery(`in:shared system:"SBARC Hub" org:sbarc`), nil, 200, &list)
	if list.Total != 1 {
		t.Errorf("system: and org: = %d", list.Total)
	}
	var detail struct {
		Channels []Channel `json:"channels"`
		Systems  []System  `json:"systems"`
	}
	c.do("GET", "/organizations/sbarc", nil, 200, &detail)
	if len(detail.Channels) != 1 || len(detail.Systems) != 1 {
		t.Errorf("org detail = %+v", detail)
	}
	if code := c.errCode("DELETE", "/organizations/SBARC", nil, 409); code != "conflict" {
		t.Errorf("delete attached: %s", code)
	}

	var plan struct {
		Plan Plan      `json:"plan"`
		Rows []PlanRow `json:"rows"`
	}
	c.do("POST", "/plans", map[string]any{"name": "Fair", "starts": "2026-07-15", "status": "published", "reason": "pilot",
		"entries": []map[string]any{{"channel_id": shared, "name": "FAIR TAC1", "assignment": "Tactical"}, {"channel_id": nil, "query": "name:70cm mode:fm", "section_start": 5}}}, 201, &plan)
	if len(plan.Rows) != 2 || plan.Rows[0].Name != "FAIR TAC1" || plan.Rows[1].Number != 5 || *plan.Plan.Starts != "2026-07-15" {
		t.Fatalf("plan = %+v", plan)
	}
	c.do("POST", L+"/radios", map[string]any{"name": "HT", "model": "baofeng-uv5rm"}, 201, nil)
	var sources struct {
		Sources []Source `json:"sources"`
	}
	c.do("POST", L+"/radios/HT/sections", map[string]any{"name": "Fair", "start": 50}, 201, nil)
	c.do("POST", L+"/radios/HT/sources", map[string]any{"plan": "fair", "section": "Fair", "plan_numbers": "match"}, 201, &sources)
	if sources.Sources[0].Plan == nil || *sources.Sources[0].Plan != "Fair" {
		t.Errorf("sources = %+v", sources)
	}
	var synced struct {
		Added int `json:"added"`
	}
	c.do("POST", L+"/radios/HT/sync", map[string]any{}, 200, &synced)
	if synced.Added != 2 {
		t.Errorf("sync added %d", synced.Added)
	}
	var mems struct {
		Memories []Memory `json:"memories"`
	}
	c.do("GET", L+"/radios/HT/memories", nil, 200, &mems)
	if len(mems.Memories) != 2 || *mems.Memories[0].Memory != 50 || *mems.Memories[1].Memory != 54 {
		t.Errorf("memories = %+v", mems.Memories)
	}
	c.do("PATCH", "/plans/Fair", map[string]any{"status": "archived", "reason": "over"}, 200, &plan)
	var revs struct {
		Revisions []RecordRevision `json:"revisions"`
	}
	c.do("GET", "/plans/Fair/history", nil, 200, &revs)
	if len(revs.Revisions) != 2 || strings.Join(revs.Revisions[1].Changes, ";") != "status: published → archived" {
		t.Errorf("history = %+v", revs)
	}
	c.do("DELETE", "/systems/SBARC%20Hub", nil, 204, nil)
}

// Private channels that match shared ones are found with a dry run, and
// only the ones picked become links.
func TestConvertPickedChannels(t *testing.T) {
	c := newClient(t, "")
	const L = "/libraries/default"
	c.do("POST", "/seeds/us-noaa/update", map[string]any{}, 200, nil)
	var a, b channelResult
	c.do("POST", L+"/channels", map[string]any{"name": "Weather 1", "rx_hz": 162_550_000, "mode": "FM", "service": "noaa", "rx_only": true}, 201, &a)
	c.do("POST", L+"/channels", map[string]any{"name": "Weather 2", "rx_hz": 162_400_000, "mode": "FM", "service": "noaa", "rx_only": true}, 201, &b)
	var found struct {
		Conversions []Conversion `json:"conversions"`
	}
	c.do("POST", L+"/shared/convert", map[string]any{"dry_run": true}, 200, &found)
	if len(found.Conversions) != 2 {
		t.Fatalf("dry run = %+v", found.Conversions)
	}
	c.do("POST", L+"/shared/convert", map[string]any{"ids": []string{a.Channel.ID}}, 200, &found)
	if len(found.Conversions) != 1 || found.Conversions[0].Channel.ID != a.Channel.ID || found.Conversions[0].Link.Link == nil {
		t.Fatalf("converted = %+v", found.Conversions)
	}
	var left Channel
	c.do("GET", L+"/channels/"+b.Channel.ID, nil, 200, &left)
	if left.Link != nil {
		t.Errorf("Weather 2 was linked without being picked: %+v", left.Link)
	}
}

// A source preview lists what the source would add after the radio's
// sources claim theirs, without adding it.
func TestPreviewSource(t *testing.T) {
	c := newClient(t, "")
	const L = "/libraries/default"
	c.seedAndLink("us-noaa", "us-gmrs", "us-ham-calling")
	c.do("POST", L+"/radios", map[string]any{"name": "HT", "model": "baofeng-uv5rm"}, 201, nil)
	c.do("POST", L+"/radios/HT/sources", map[string]any{"query": "band:noaa"}, 201, nil)
	var p struct {
		Added     int       `json:"added"`
		Channels  []Channel `json:"channels"`
		Skipped   []Channel `json:"skipped"`
		WrongMode []Channel `json:"wrong_mode"`
	}
	c.do("POST", L+"/radios/HT/sources/preview", map[string]any{"query": `band:noaa OR service:murs OR name:"6m FM Call" OR name:"2m FM Call"`}, 200, &p)
	if p.Added != 1 || len(p.Channels) != 1 || p.Channels[0].Name != "2m FM Call" {
		t.Errorf("would add %d: %+v (NOAA belongs to source 1)", p.Added, p.Channels)
	}
	if len(p.Skipped) != 1 || p.Skipped[0].Name != "6m FM Call" {
		t.Errorf("skipped = %+v", p.Skipped)
	}
	var srcs struct {
		Sources []Source `json:"sources"`
	}
	c.do("GET", L+"/radios/HT/sources", nil, 200, &srcs)
	if len(srcs.Sources) != 1 {
		t.Errorf("the preview added a source: %+v", srcs.Sources)
	}
	if code := c.errCode("POST", L+"/radios/HT/sources/preview", map[string]any{"query": "band:nowhere"}, 400); code != "invalid" {
		t.Errorf("bad query: %s", code)
	}
	// An existing source previews in its place, with the changes given:
	// source 1 widened to the calling channels finds those not yet on HT.
	c.do("POST", L+"/radios/HT/sources/preview", map[string]any{"number": 1, "query": `band:noaa OR name:"2m FM Call"`}, 200, &p)
	if p.Added != 8 || len(p.Channels) != 8 {
		t.Errorf("edit preview = %d: %+v", p.Added, p.Channels)
	}
	c.do("POST", L+"/radios/HT/sources/preview", map[string]any{"number": 1}, 200, &p)
	if p.Added != 7 {
		t.Errorf("unchanged source preview = %d", p.Added)
	}
	if code := c.errCode("POST", L+"/radios/HT/sources/preview", map[string]any{"number": 9}, 400); code != "invalid" {
		t.Errorf("missing source: %s", code)
	}
}

// A radio compares with its file, and decisions bring rflib in line.
func TestCompareRadio(t *testing.T) {
	c := newClient(t, "")
	const L = "/libraries/default"
	c.seedAndLink("us-ham-calling")
	c.do("POST", L+"/radios", map[string]any{"name": "HT", "model": "baofeng-uv5rm"}, 201, nil)
	file := "Location,Name,Frequency,Duplex,Offset,Tone,rToneFreq,cToneFreq,DtcsCode,DtcsPolarity,RxDtcsCode,CrossMode,Mode,TStep,Skip,Power,Comment,URCALL,RPT1CALL,RPT2CALL,DVCODE\n" +
		"3,2m Call,146.520000,,0.000000,,88.5,88.5,023,NN,023,Tone->Tone,FM,5.00,,,,,,,\n" +
		"8,Mystery,146.580000,,0.000000,,88.5,88.5,023,NN,023,Tone->Tone,FM,5.00,,,,,,,\n"
	var got struct {
		Memories []struct {
			Memory     *int   `json:"memory"`
			Status     string `json:"status"`
			Candidates []struct {
				Match   string  `json:"match"`
				Channel Channel `json:"channel"`
			} `json:"candidates"`
		} `json:"memories"`
	}
	c.do("POST", L+"/radios/HT/compare", map[string]any{"file": file}, 200, &got)
	if len(got.Memories) != 2 || got.Memories[0].Status != "radio" || len(got.Memories[0].Candidates) == 0 || got.Memories[0].Candidates[0].Match != "exact" {
		t.Fatalf("compare = %+v", got)
	}
	call := got.Memories[0].Candidates[0].Channel
	body := map[string]any{"file": file, "use": []map[string]any{{"memory": 3, "channel_id": call.ID}}, "keep": []int{8}, "dry_run": true}
	var res struct {
		Used, Kept int
		DryRun     bool `json:"dry_run"`
	}
	c.do("POST", L+"/radios/HT/compare/apply", body, 200, &res)
	if res.Used != 1 || res.Kept != 1 || !res.DryRun {
		t.Fatalf("dry run = %+v", res)
	}
	c.do("POST", L+"/radios/HT/compare", map[string]any{"file": file}, 200, &got)
	if got.Memories[1].Status != "radio" {
		t.Fatalf("a dry run changed the radio: %+v", got)
	}
	delete(body, "dry_run")
	c.do("POST", L+"/radios/HT/compare/apply", body, 200, &res)
	c.do("POST", L+"/radios/HT/compare", map[string]any{"file": file}, 200, &got)
	for _, m := range got.Memories {
		if m.Status == "radio" || m.Status == "rflib" {
			t.Errorf("after applying, memory %v is %s", m.Memory, m.Status)
		}
	}
	if code := c.errCode("POST", L+"/radios/HT/compare/apply", map[string]any{"file": file, "take": []int{5}}, 400); code != "invalid" {
		t.Errorf("take a memory that isn't changed: %s", code)
	}
}

// An empty search is the default one, the channels in use, and the list can
// say how many more the shared list matches.
func TestChannelListDefaultAndMoreInShared(t *testing.T) {
	c := newClient(t, "")
	const L = "/libraries/default"
	noaa := c.seedAndLink("us-noaa")
	c.do("POST", L+"/radios", map[string]any{"name": "HT", "model": "baofeng-uv5rm"}, 201, nil)
	c.do("POST", L+"/radios/HT/channels", map[string]any{"ids": []string{noaa[0].ID}}, 200, nil)
	var list struct {
		Total int `json:"total"`
		More  int `json:"more_in_shared"`
	}
	for _, q := range []string{"", "&q=" + urlQuery("in:use")} {
		c.do("GET", L+"/channels?more_in_shared=true"+q, nil, 200, &list)
		if list.Total != 1 || list.More != len(noaa)-1 {
			t.Errorf("%q: total %d, more %d; want 1 and %d", q, list.Total, list.More, len(noaa)-1)
		}
	}
	c.do("GET", L+"/channels?more_in_shared=true&q="+urlQuery("in:all"), nil, 200, &list)
	if list.Total != len(noaa) || list.More != 0 {
		t.Errorf("in:all: total %d, more %d", list.Total, list.More)
	}
	c.do("GET", L+"/channels", nil, 200, &list)
	if list.More != 0 {
		t.Errorf("uncounted unless asked: %d", list.More)
	}
}

// A radio copies to another model, and two radios compare by channel.
func TestCopyAndCompareRadios(t *testing.T) {
	c := newClient(t, "")
	const L = "/libraries/default"
	c.seedAndLink("us-noaa", "us-ham-calling")
	c.do("POST", L+"/radios", map[string]any{"name": "Shack", "model": "chirp-generic"}, 201, nil)
	c.do("POST", L+"/radios/Shack/sections", map[string]any{"name": "Weather", "start": 20}, 201, nil)
	c.do("POST", L+"/radios/Shack/channels", map[string]any{"query": "band:noaa", "section": "Weather"}, 200, nil)
	c.do("POST", L+"/radios/Shack/channels", map[string]any{"query": `name:"6m FM Call" OR name:"2m FM Call"`}, 200, nil)
	var copied struct {
		Radio   Radio     `json:"radio"`
		Copied  int       `json:"copied"`
		Skipped []Channel `json:"skipped"`
	}
	c.do("POST", L+"/radios/Shack/copy", map[string]any{"name": "Mobile", "model": "baofeng-uv5rm"}, 201, &copied)
	if copied.Radio.Name != "Mobile" || copied.Copied != 8 || len(copied.Skipped) != 1 || copied.Skipped[0].Name != "6m FM Call" {
		t.Fatalf("copy = %+v", copied)
	}
	var cmp struct {
		Same    int `json:"same"`
		Missing []struct {
			Channel  Channel `json:"channel"`
			Section  string  `json:"section"`
			Unusable *string `json:"unusable"`
		} `json:"missing"`
	}
	c.do("POST", L+"/radios/Mobile/compare-radio", map[string]any{"radio": "Shack"}, 200, &cmp)
	if cmp.Same != 8 || len(cmp.Missing) != 1 || cmp.Missing[0].Unusable == nil {
		t.Fatalf("compare = %+v", cmp)
	}
	var added struct {
		Added []Channel `json:"added"`
	}
	c.do("POST", L+"/radios/Mobile/compare-radio/apply", map[string]any{"radio": "Shack"}, 200, &added)
	if len(added.Added) != 0 {
		t.Errorf("added what the radio can't use: %+v", added)
	}
}
