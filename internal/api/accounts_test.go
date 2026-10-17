package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/ryan/rflib/internal/app"
	"github.com/ryan/rflib/internal/domain"
	"github.com/ryan/rflib/internal/store/sqlite"
)

type authInfo struct {
	Accounts bool   `json:"accounts"`
	Signup   string `json:"signup"`
	User     *User  `json:"user"`
}

// accountsServer serves a database with an admin, "n0call", and sign-up
// as given.
func accountsServer(t *testing.T, signup app.Signup) (*client, *sqlite.DB) {
	t.Helper()
	c, d := newServer(t, Options{Accounts: &AccountOptions{Signup: signup}})
	if _, err := app.NewAccounts(d).AddUser(context.Background(), domain.User{Name: "N0CALL", Role: domain.RoleAdmin}, "admin password"); err != nil {
		t.Fatal(err)
	}
	return c, d
}

func (c *client) signIn(name, password string) User {
	c.t.Helper()
	var out struct{ User User }
	c.do("POST", "/auth/signin", map[string]any{"name": name, "password": password}, 200, &out)
	return out.User
}

func TestAccountsSignInAndRegister(t *testing.T) {
	admin, _ := accountsServer(t, app.SignupOpen)
	var info authInfo
	admin.do("GET", "/auth", nil, 200, &info)
	if !info.Accounts || info.Signup != "open" || info.User != nil {
		t.Fatalf("auth = %+v", info)
	}
	if code := admin.errCode("GET", "/libraries", nil, 401); code != "unauthorized" {
		t.Errorf("signed out: %s", code)
	}
	if code := admin.errCode("POST", "/auth/signin", map[string]any{"name": "n0call", "password": "wrong password"}, 401); code != "unauthorized" {
		t.Errorf("wrong password: %s", code)
	}
	if code := admin.errCode("POST", "/auth/signin", map[string]any{"name": "nobody", "password": "admin password"}, 401); code != "unauthorized" {
		t.Errorf("no such user: %s", code)
	}
	// The first admin took the library there was before accounts.
	if u := admin.signIn("n0call", "admin password"); u.Role != "admin" || u.Library != "default" || !u.Callsign {
		t.Fatalf("admin = %+v", u)
	}
	admin.do("GET", "/auth", nil, 200, &info)
	if info.User == nil || info.User.Name != "N0CALL" {
		t.Fatalf("signed in: %+v", info)
	}

	member := admin.another()
	if code := member.errCode("POST", "/auth/register", map[string]any{"name": "shared", "password": "a long password"}, 400); code != "invalid" {
		t.Errorf("reserved name: %s", code)
	}
	var reg struct{ User User }
	member.do("POST", "/auth/register", map[string]any{"name": "Wx1yz", "password": "a long password", "display_name": "Pat"}, 201, &reg)
	if reg.User.Role != "member" || reg.User.Library != "wx1yz" {
		t.Fatalf("registered = %+v", reg.User)
	}
	if code := member.another().errCode("POST", "/auth/register", map[string]any{"name": "wx1YZ", "password": "a long password"}, 409); code != "conflict" {
		t.Errorf("taken name: %s", code)
	}

	// Each sees their own library and the shared list, and nobody else's.
	var libs struct{ Libraries []Library }
	member.do("GET", "/libraries", nil, 200, &libs)
	names := map[string]bool{}
	for _, l := range libs.Libraries {
		names[l.Name] = l.Default
	}
	if len(names) != 2 || !names["wx1yz"] || names["shared"] {
		t.Fatalf("member's libraries = %+v", libs.Libraries)
	}
	admin.do("POST", "/libraries/default/channels", map[string]any{"name": "Mine", "rx_hz": 146520000, "mode": "fm"}, 201, nil)
	if code := member.errCode("GET", "/libraries/default/channels", nil, 404); code != "not_found" {
		t.Errorf("another's library: %s", code)
	}
	member.do("POST", "/libraries/wx1yz/channels", map[string]any{"name": "Theirs", "rx_hz": 146550000, "mode": "fm"}, 201, nil)
	var chans struct{ Channels []Channel }
	member.do("GET", "/libraries/wx1yz/channels?q=in:mine", nil, 200, &chans)
	if len(chans.Channels) != 1 || chans.Channels[0].Name != "Theirs" {
		t.Errorf("member's channels = %+v", chans.Channels)
	}
	// An admin may open any library, for support.
	admin.do("GET", "/libraries/wx1yz/channels", nil, 200, nil)

	// A page on another site can't make changes with the cookie.
	member.origin = "https://elsewhere.example"
	if code := member.errCode("POST", "/libraries/wx1yz/channels", map[string]any{"name": "Forged", "rx_hz": 146580000, "mode": "fm"}, 403); code != "forbidden" {
		t.Errorf("cross-site: %s", code)
	}
	member.origin = ""
	req, _ := newRequest("GET", member.srv.URL+Prefix+"/libraries", "")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	if resp, err := member.http.Do(req); err != nil || resp.StatusCode != 403 {
		t.Errorf("a link from another site: %v %v", resp, err)
	} else {
		_ = resp.Body.Close()
	}
	member.origin = member.srv.URL
	member.do("POST", "/libraries/wx1yz/channels", map[string]any{"name": "Same site", "rx_hz": 146580000, "mode": "fm"}, 201, nil)

	// Changing the password signs out the other sessions, not this one.
	other := member.another()
	other.signIn("wx1yz", "a long password")
	if code := member.errCode("POST", "/auth/password", map[string]any{"current": "wrong", "password": "another long one"}, 400); code != "invalid" {
		t.Errorf("wrong current password: %s", code)
	}
	member.do("POST", "/auth/password", map[string]any{"current": "a long password", "password": "another long one"}, 204, nil)
	member.do("GET", "/libraries", nil, 200, nil)
	other.errCode("GET", "/libraries", nil, 401)
	var me struct{ User User }
	member.do("PATCH", "/auth/me", map[string]any{"email": "pat@example.org"}, 200, &me)
	if me.User.Email != "pat@example.org" || me.User.DisplayName != "Pat" {
		t.Errorf("me = %+v", me.User)
	}
	member.do("POST", "/auth/signout", nil, 204, nil)
	member.errCode("GET", "/libraries", nil, 401)
}

func TestAccountsSignupSettings(t *testing.T) {
	c, d := accountsServer(t, app.SignupInvite)
	body := map[string]any{"name": "n1call", "password": "a long password"}
	if code := c.errCode("POST", "/auth/register", body, 400); code != "invalid" {
		t.Errorf("no code: %s", code)
	}
	body["invite"] = "not a code"
	if code := c.errCode("POST", "/auth/register", body, 404); code != "not_found" {
		t.Errorf("bad code: %s", code)
	}
	code, _, err := app.NewAccounts(d).Invite(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	body["invite"] = code
	c.do("POST", "/auth/register", body, 201, nil)
	body["name"] = "n2call"
	if got := c.another().errCode("POST", "/auth/register", body, 404); got != "not_found" {
		t.Errorf("spent code: %s", got)
	}

	closed, _ := accountsServer(t, app.SignupClosed)
	if got := closed.errCode("POST", "/auth/register", body, 403); got != "forbidden" {
		t.Errorf("closed: %s", got)
	}

	// Without accounts, the account endpoints aren't there.
	plain := newClient(t, "")
	var info authInfo
	plain.do("GET", "/auth", nil, 200, &info)
	if info.Accounts {
		t.Errorf("auth = %+v", info)
	}
	plain.errCode("POST", "/auth/signin", map[string]any{"name": "a", "password": "b"}, 404)
}

func TestSignInThrottled(t *testing.T) {
	c, _ := accountsServer(t, app.SignupClosed)
	for range 5 {
		c.errCode("POST", "/auth/signin", map[string]any{"name": "n0call", "password": "wrong password"}, 401)
	}
	// Even the right password waits out the window now.
	if code := c.errCode("POST", "/auth/signin", map[string]any{"name": "n0call", "password": "admin password"}, 429); code != "too_many" {
		t.Errorf("throttled: %s", code)
	}
}

// A member reads the shared list but changes nothing on it, by any
// endpoint: the maintainer's changes are refused, and every other change
// to the shared list or someone else's library fails without changing it.
func TestMembersDontChangeTheSharedList(t *testing.T) {
	admin, _ := accountsServer(t, app.SignupOpen)
	admin.signIn("n0call", "admin password")
	admin.do("POST", "/seeds/us-noaa/update", map[string]any{}, 200, nil)
	var wx struct{ Channels []Channel }
	admin.do("GET", "/libraries/shared/channels?q=service:noaa", nil, 200, &wx)
	shared := wx.Channels[0].ID
	admin.do("POST", "/organizations", map[string]any{"name": "SBARC", "reason": "pilot"}, 201, nil)
	admin.do("POST", "/plans", map[string]any{"name": "Fair", "status": "draft", "reason": "pilot", "entries": []map[string]any{{"channel_id": shared}}}, 201, nil)
	admin.do("POST", "/libraries/default/radios", map[string]any{"name": "Admin HT", "model": "chirp-generic"}, 201, nil)
	snapshot := func() string {
		var s string
		admin.do("GET", "/libraries/shared/exports/json", nil, 200, &s)
		var radios string
		admin.do("GET", "/libraries/default/radios", nil, 200, &radios)
		return s + radios
	}
	before := snapshot()

	member := admin.another()
	member.do("POST", "/auth/register", map[string]any{"name": "wx1yz", "password": "a long password"}, 201, nil)
	member.do("GET", "/libraries/shared/channels", nil, 200, nil)
	member.do("GET", "/organizations", nil, 200, nil)
	for _, w := range []struct {
		method, path string
		body         map[string]any
	}{
		{"PATCH", "/libraries/shared/channels/" + shared, map[string]any{"notes": "mine now", "reason": "test"}},
		{"POST", "/libraries/shared/channels/" + shared + "/status", map[string]any{"status": "off-air", "reason": "test"}},
		{"POST", "/organizations", map[string]any{"name": "Mine", "reason": "test"}},
		{"PATCH", "/organizations/SBARC", map[string]any{"notes": "x", "reason": "test"}},
		{"DELETE", "/organizations/SBARC", nil},
		{"PATCH", "/plans/Fair", map[string]any{"status": "archived", "reason": "test"}},
		{"POST", "/libraries/shared/channels/bulk-status", map[string]any{"select": map[string]any{"ids": []string{shared}}, "status": "retired", "reason": "test"}},
		{"POST", "/libraries/shared/channels/bulk-update", map[string]any{"select": map[string]any{"ids": []string{shared}}, "edit": map[string]any{"tags": map[string]any{"add": []string{"wx"}}}}},
		{"POST", "/systems", map[string]any{"name": "Mine", "reason": "test"}},
		{"POST", "/plans", map[string]any{"name": "Mine", "status": "draft", "reason": "test"}},
		{"POST", "/seeds/us-noaa/update", map[string]any{}},
	} {
		if code := member.errCode(w.method, w.path, w.body, 403); code != "forbidden" && code != "read_only" {
			t.Errorf("%s %s = %s", w.method, w.path, code)
		}
	}
	// Every other change, on the shared list and the admin's library,
	// with what IDs there are; none may succeed.
	params := map[string]string{"{lib}": "shared", "{id}": shared, "{org}": "SBARC", "{plan}": "Fair", "{seed}": "us-noaa",
		"{system}": "none", "{radio}": "Admin HT", "{n}": "1", "{section}": "Other", "{name}": "x", "{format}": "json"}
	for _, lib := range []string{"shared", "default"} {
		params["{lib}"] = lib
		for _, rt := range Routes() {
			if rt.Method == "GET" || strings.HasPrefix(rt.Path, "/auth") {
				continue
			}
			path := rt.Path
			for k, v := range params {
				path = strings.ReplaceAll(path, k, strings.ReplaceAll(v, " ", "%20"))
			}
			status := member.status(rt.Method, path)
			if status < 400 || status >= 500 {
				t.Errorf("%s %s = %d", rt.Method, path, status)
			}
		}
	}
	if snapshot() != before {
		t.Error("a member's request changed the shared list or the admin's radios")
	}
}

// status sends an empty JSON object and returns the status, without
// holding the request to the spec: the sweep sends what no endpoint
// expects.
func (c *client) status(method, path string) int {
	c.t.Helper()
	req, err := newRequest(method, c.srv.URL+Prefix+path, "{}")
	if err != nil {
		c.t.Fatal(err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	_ = resp.Body.Close()
	return resp.StatusCode
}

func newRequest(method, url, body string) (*http.Request, error) {
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err == nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, err
}
