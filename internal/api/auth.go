package api

import (
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/ryan/rflib/internal/app"
	"github.com/ryan/rflib/internal/domain"
)

// AccountOptions put the API behind sign-in: each request but the public
// ones runs as the user of its session cookie (see app.WithUser).
type AccountOptions struct {
	// Signup is who may register.
	Signup app.Signup
	// Secure marks the session cookie HTTPS-only. Clear it only for plain
	// HTTP, such as on a mesh network without TLS.
	Secure bool
	// TrustProxy takes the client's address from X-Forwarded-For, for a
	// server behind a proxy that sets it, so throttling counts clients and
	// not the proxy.
	TrustProxy bool
}

// sessionCookie holds a browser's session token.
const sessionCookie = "rflib_session"

// User is the signed-in user, as the API shows them.
type User struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
	Role        string `json:"role"`
	Callsign    bool   `json:"callsign"`
	Library     string `json:"library"`
}

func newUser(u domain.User, library string) User {
	return User{u.ID, u.Name, u.DisplayName, u.Email, string(u.Role), u.Callsign(), library}
}

// authenticate runs r as its session's user, or reports why it can't.
func (s *server) authenticate(r *http.Request) (*http.Request, error) {
	c, _ := r.Cookie(sessionCookie)
	token := ""
	if c != nil {
		token = c.Value
	}
	u, err := s.accounts.SessionUser(r.Context(), token)
	if err != nil {
		return r, err
	}
	return r.WithContext(app.WithUser(r.Context(), u)), nil
}

// sameOrigin refuses a request sent from another site's page: a session
// cookie goes with requests to the server from anywhere, so requests must
// come from the server's own pages. Browsers mark a request from another
// site in Sec-Fetch-Site, even a link followed (some GETs, such as an
// export, record a revision), and send Origin with every change; clients
// that aren't browsers send neither.
func sameOrigin(r *http.Request) error {
	refuse := apiError{http.StatusForbidden, "forbidden", "requests must come from this server's own pages"}
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return refuse
	}
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		return nil
	}
	o := r.Header.Get("Origin")
	if o == "" {
		return nil
	}
	if u, err := url.Parse(o); err == nil && strings.EqualFold(u.Host, r.Host) {
		return nil
	}
	return refuse
}

func (s *server) setSession(w http.ResponseWriter, token string, maxAge int) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: token, Path: "/", MaxAge: maxAge,
		HttpOnly: true, Secure: s.opts.Accounts.Secure, SameSite: http.SameSiteLaxMode})
}

// noAccounts is an account endpoint on a server without accounts.
var noAccounts = apiError{http.StatusNotFound, "not_found", "this server has no accounts; start it with rflib serve --accounts"}

// authInfo says whether the server has accounts, who may register, and
// who is signed in.
func (s *server) authInfo(w http.ResponseWriter, r *http.Request) error {
	out := map[string]any{"accounts": s.opts.Accounts != nil}
	if s.opts.Accounts != nil {
		out["signup"] = string(s.opts.Accounts.Signup)
		if r2, err := s.authenticate(r); err == nil {
			u, _ := app.UserOf(r2.Context())
			me, err := s.me(r2, u)
			if err != nil {
				return err
			}
			out["user"] = me
		}
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

func (s *server) me(r *http.Request, u domain.User) (User, error) {
	lib, err := s.st.GetLibrary(ctx(r), u.LibraryID)
	if err != nil {
		return User{}, err
	}
	return newUser(u, lib.Name), nil
}

func (s *server) signIn(w http.ResponseWriter, r *http.Request) error {
	if s.opts.Accounts == nil {
		return noAccounts
	}
	var in struct {
		Name     string `json:"name"`
		Password string `json:"password"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	keys := []string{"name:" + strings.ToLower(strings.TrimSpace(in.Name)), "ip:" + s.client(r)}
	if err := s.throttle.check(keys...); err != nil {
		return err
	}
	u, token, err := s.accounts.SignIn(ctx(r), in.Name, in.Password)
	if err != nil {
		s.throttle.fail(keys...)
		return err
	}
	s.throttle.clear(keys[0])
	s.setSession(w, token, int(app.SessionTTL/time.Second))
	me, err := s.me(r, u)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": me})
	return nil
}

func (s *server) register(w http.ResponseWriter, r *http.Request) error {
	if s.opts.Accounts == nil {
		return noAccounts
	}
	var in struct {
		Name        string `json:"name"`
		Password    string `json:"password"`
		DisplayName string `json:"display_name"`
		Email       string `json:"email"`
		Invite      string `json:"invite"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	key := "register:" + s.client(r)
	if err := s.throttle.check(key); err != nil {
		return err
	}
	// Every attempt counts, so one client can't make accounts without end.
	s.throttle.fail(key)
	u := domain.User{Name: strings.TrimSpace(in.Name), DisplayName: strings.TrimSpace(in.DisplayName), Email: strings.TrimSpace(in.Email)}
	if _, err := s.accounts.Register(ctx(r), s.opts.Accounts.Signup, u, in.Password, in.Invite); err != nil {
		return err
	}
	u, token, err := s.accounts.SignIn(ctx(r), u.Name, in.Password)
	if err != nil {
		return err
	}
	s.setSession(w, token, int(app.SessionTTL/time.Second))
	me, err := s.me(r, u)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, map[string]any{"user": me})
	return nil
}

func (s *server) signOut(w http.ResponseWriter, r *http.Request) error {
	if s.opts.Accounts == nil {
		return noAccounts
	}
	if c, err := r.Cookie(sessionCookie); err == nil {
		if err := s.accounts.SignOut(ctx(r), c.Value); err != nil {
			return err
		}
	}
	s.setSession(w, "", -1)
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *server) updateMe(w http.ResponseWriter, r *http.Request) error {
	if s.opts.Accounts == nil {
		return noAccounts
	}
	var in struct {
		DisplayName *string `json:"display_name"`
		Email       *string `json:"email"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	u, _ := app.UserOf(ctx(r))
	display, email := u.DisplayName, u.Email
	if in.DisplayName != nil {
		display = *in.DisplayName
	}
	if in.Email != nil {
		email = *in.Email
	}
	u, err := s.accounts.UpdateProfile(ctx(r), display, email)
	if err != nil {
		return err
	}
	me, err := s.me(r, u)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": me})
	return nil
}

func (s *server) changePassword(w http.ResponseWriter, r *http.Request) error {
	if s.opts.Accounts == nil {
		return noAccounts
	}
	var in struct {
		Current  string `json:"current"`
		Password string `json:"password"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	c, _ := r.Cookie(sessionCookie)
	token := ""
	if c != nil {
		token = c.Value
	}
	if err := s.accounts.ChangePassword(ctx(r), in.Current, in.Password, token); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// client is the address a request came from.
func (s *server) client(r *http.Request) string {
	if s.opts.Accounts != nil && s.opts.Accounts.TrustProxy {
		// The proxy appends the address it saw, so the last one is the
		// only one it vouches for.
		if f := r.Header.Get("X-Forwarded-For"); f != "" {
			parts := strings.Split(f, ",")
			return strings.TrimSpace(parts[len(parts)-1])
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// throttle counts failures by key (a name, or a client's address) and
// refuses a key with too many in its window until the window ends.
type throttle struct {
	mu     sync.Mutex
	window time.Duration
	limits map[string]int
	seen   map[string]*attempts
}

type attempts struct {
	n     int
	since time.Time
}

func newThrottle() *throttle {
	return &throttle{window: 15 * time.Minute, seen: map[string]*attempts{},
		limits: map[string]int{"name": 5, "ip": 30, "register": 10}}
}

func (t *throttle) limit(key string) int {
	kind, _, _ := strings.Cut(key, ":")
	return t.limits[kind]
}

func (t *throttle) check(keys ...string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	for _, k := range keys {
		a := t.seen[k]
		if a != nil && now.Sub(a.since) >= t.window {
			delete(t.seen, k)
			continue
		}
		if a != nil && a.n >= t.limit(k) {
			return apiError{http.StatusTooManyRequests, "too_many", "too many attempts; try again later"}
		}
	}
	return nil
}

func (t *throttle) fail(keys ...string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	for _, k := range keys {
		a := t.seen[k]
		if a == nil || now.Sub(a.since) >= t.window {
			a = &attempts{since: now}
			t.seen[k] = a
		}
		a.n++
	}
	// Forget old windows, so the map doesn't grow without end.
	if len(t.seen) > 10000 {
		for k, a := range t.seen {
			if now.Sub(a.since) >= t.window {
				delete(t.seen, k)
			}
		}
	}
}

func (t *throttle) clear(key string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.seen, key)
}
