package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/argon2"

	"github.com/ryan/rflib/internal/domain"
	"github.com/ryan/rflib/internal/store"
)

// On a server with accounts, each request runs as its signed-in user (see
// WithUser): a member opens only their own library and the shared list,
// which they read but don't change; an admin also maintains the shared list
// and may open any library, for support. Without a user (the CLI, and
// rflib serve without accounts) everything is allowed, as before.

type userKey struct{}

// WithUser makes ctx a request by u, who is also the author of its
// changes.
func WithUser(ctx context.Context, u domain.User) context.Context {
	return store.WithAuthor(context.WithValue(ctx, userKey{}, u), u.Name)
}

// UserOf returns the user WithUser attached to ctx.
func UserOf(ctx context.Context) (domain.User, bool) {
	u, ok := ctx.Value(userKey{}).(domain.User)
	return u, ok
}

// ErrForbidden is a user doing what their role doesn't allow (HTTP 403).
var ErrForbidden = errors.New("forbidden")

// ErrUnauthorized is a request without a valid session, or a failed
// sign-in (HTTP 401).
var ErrUnauthorized = errors.New("unauthorized")

// kindError is a message that is one of ErrForbidden or ErrUnauthorized.
type kindError struct {
	msg  string
	kind error
}

func (e kindError) Error() string        { return e.msg }
func (e kindError) Is(target error) bool { return target == e.kind }

func forbidden(msg string) error    { return kindError{msg, ErrForbidden} }
func unauthorized(msg string) error { return kindError{msg, ErrUnauthorized} }

// maintaining refuses a change to the shared list from a user who isn't
// an admin.
func maintaining(ctx context.Context) error {
	if u, ok := UserOf(ctx); ok && !u.Admin() {
		return forbidden("only an admin can change the shared list")
	}
	return nil
}

// administering refuses account administration from a user who isn't an
// admin.
func administering(ctx context.Context) error {
	if u, ok := UserOf(ctx); ok && !u.Admin() {
		return forbidden("only an admin can manage accounts")
	}
	return nil
}

// visible reports whether ctx's user may open lib.
func visible(ctx context.Context, lib domain.Library) bool {
	u, ok := UserOf(ctx)
	return !ok || lib.Shared || lib.OwnerID == u.ID || u.Admin()
}

// Accounts are a server's users and their sessions.
type Accounts struct{ st store.Store }

// NewAccounts returns the accounts kept in st.
func NewAccounts(st store.Store) *Accounts { return &Accounts{st: st} }

// SessionTTL is how long a session lasts unused.
const SessionTTL = 30 * 24 * time.Hour

// InviteTTL is how long an invitation code lasts.
const InviteTTL = 14 * 24 * time.Hour

// Signup is who may register on a server.
type Signup string

const (
	SignupOpen   Signup = "open"
	SignupInvite Signup = "invite"
	SignupClosed Signup = "closed"
)

// ParseSignup reads a sign-up setting.
func ParseSignup(s string) (Signup, error) {
	switch v := Signup(strings.ToLower(s)); v {
	case SignupOpen, SignupInvite, SignupClosed:
		return v, nil
	}
	return "", domain.Invalidf("sign-up is open, invite, or closed, not %q", s)
}

// HasAdmin reports whether any admin exists.
func (a *Accounts) HasAdmin(ctx context.Context) (bool, error) {
	us, err := a.st.ListUsers(ctx)
	if err != nil {
		return false, err
	}
	for _, u := range us {
		if u.Admin() && !u.Disabled {
			return true, nil
		}
	}
	return false, nil
}

// AddUser makes an account, as an admin does. The first account is an
// admin and takes the default library, which held everything before there
// were accounts; later ones get a new library each.
func (a *Accounts) AddUser(ctx context.Context, u domain.User, password string) (domain.User, error) {
	opts, err := a.addOptions(ctx, u)
	if err != nil {
		return u, err
	}
	return a.create(ctx, u, password, opts)
}

// CheckNewUser reports what would stop AddUser adding u, but for the
// password, so it can be asked for after.
func (a *Accounts) CheckNewUser(ctx context.Context, u domain.User) error {
	_, err := a.addOptions(ctx, u)
	return err
}

func (a *Accounts) addOptions(ctx context.Context, u domain.User) (store.NewUser, error) {
	var opts store.NewUser
	if err := administering(ctx); err != nil {
		return opts, err
	}
	if err := domain.ValidateUser(u); err != nil {
		return opts, err
	}
	if _, err := a.st.GetUser(ctx, u.Name); err == nil {
		return opts, fmt.Errorf("the name %q is taken: %w", u.Name, store.ErrConflict)
	}
	us, err := a.st.ListUsers(ctx)
	if err != nil {
		return opts, err
	}
	if len(us) == 0 {
		if u.Role != domain.RoleAdmin {
			return opts, domain.Invalidf("the first account is the admin's, which takes the library you have now; add it with --admin")
		}
		lib, err := a.st.GetLibrary(ctx, "")
		if err != nil {
			return opts, err
		}
		if lib.OwnerID == "" {
			opts.Claim = lib.ID
		}
	}
	return opts, nil
}

// Register makes a member's account from the sign-up page, as signup
// allows: invite needs a valid invitation code, which this spends.
func (a *Accounts) Register(ctx context.Context, signup Signup, u domain.User, password, invite string) (domain.User, error) {
	u.Role, u.Disabled = domain.RoleMember, false
	var opts store.NewUser
	switch signup {
	case SignupOpen:
	case SignupInvite:
		if strings.TrimSpace(invite) == "" {
			return u, domain.Invalidf("registering here needs an invitation code from an admin")
		}
		opts.Invite = hashToken(strings.TrimSpace(invite))
	default:
		return u, forbidden("this server isn't taking new accounts")
	}
	ok, err := a.HasAdmin(ctx)
	if err != nil {
		return u, err
	}
	if !ok {
		return u, forbidden("this server has no admin yet")
	}
	return a.create(ctx, u, password, opts)
}

func (a *Accounts) create(ctx context.Context, u domain.User, password string, opts store.NewUser) (domain.User, error) {
	if err := domain.ValidateUser(u); err != nil {
		return u, err
	}
	if err := domain.ValidatePassword(password); err != nil {
		return u, err
	}
	if _, err := a.st.GetUser(ctx, u.Name); err == nil {
		return u, fmt.Errorf("the name %q is taken: %w", u.Name, store.ErrConflict)
	}
	h, err := hashPassword(password)
	if err != nil {
		return u, err
	}
	return a.st.CreateUser(ctx, u, h, opts)
}

// errSignIn is every failed sign-in, so a caller can't tell a wrong name
// from a wrong password.
var errSignIn = unauthorized("the name or password is wrong")

// SignIn checks a user's name and password and starts a session, whose
// token is returned for the browser's cookie.
func (a *Accounts) SignIn(ctx context.Context, name, password string) (domain.User, string, error) {
	u, err := a.st.GetUser(ctx, strings.TrimSpace(name))
	var h string
	if err == nil {
		h, err = a.st.PasswordHash(ctx, u.ID)
	}
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return u, "", err
	}
	if err != nil {
		// Spend the same time as a real check.
		h = dummyHash()
	}
	if !checkPassword(h, password) || err != nil || u.Disabled {
		return domain.User{}, "", errSignIn
	}
	token, err := a.startSession(ctx, u.ID)
	return u, token, err
}

func (a *Accounts) startSession(ctx context.Context, userID string) (string, error) {
	token, err := newToken()
	if err != nil {
		return "", err
	}
	now := time.Now().UTC()
	err = a.st.CreateSession(ctx, domain.Session{TokenHash: hashToken(token), UserID: userID, CreatedAt: now, ExpiresAt: now.Add(SessionTTL)})
	return token, err
}

// SessionUser returns the user a session token belongs to, keeping the
// session alive while it's used.
func (a *Accounts) SessionUser(ctx context.Context, token string) (domain.User, error) {
	if token == "" {
		return domain.User{}, unauthorized("sign in first")
	}
	h := hashToken(token)
	s, err := a.st.Session(ctx, h)
	if errors.Is(err, store.ErrNotFound) {
		return domain.User{}, unauthorized("the session ended; sign in again")
	}
	if err != nil {
		return domain.User{}, err
	}
	u, err := a.st.GetUser(ctx, s.UserID)
	if err != nil || u.Disabled {
		return domain.User{}, unauthorized("the session ended; sign in again")
	}
	// Extended at most once a day, not on every request.
	if exp := time.Now().UTC().Add(SessionTTL); exp.Sub(s.ExpiresAt) > 24*time.Hour {
		if err := a.st.ExtendSession(ctx, h, exp); err != nil {
			return u, err
		}
	}
	return u, nil
}

// SignOut ends a session.
func (a *Accounts) SignOut(ctx context.Context, token string) error {
	return a.st.DeleteSession(ctx, hashToken(token))
}

// ChangePassword changes ctx's user's password, given their current one,
// and signs them out everywhere but the session token.
func (a *Accounts) ChangePassword(ctx context.Context, current, password, token string) error {
	u, ok := UserOf(ctx)
	if !ok {
		return unauthorized("sign in first")
	}
	h, err := a.st.PasswordHash(ctx, u.ID)
	if err != nil {
		return err
	}
	if !checkPassword(h, current) {
		return domain.Invalidf("the current password is wrong")
	}
	return a.setPassword(ctx, u.ID, password, hashToken(token))
}

// SetPassword sets a user's password, as an admin does when they've
// forgotten it, and signs them out everywhere.
func (a *Accounts) SetPassword(ctx context.Context, ref, password string) (domain.User, error) {
	if err := administering(ctx); err != nil {
		return domain.User{}, err
	}
	u, err := a.st.GetUser(ctx, ref)
	if err != nil {
		return u, err
	}
	return u, a.setPassword(ctx, u.ID, password, "")
}

func (a *Accounts) setPassword(ctx context.Context, userID, password, keep string) error {
	if err := domain.ValidatePassword(password); err != nil {
		return err
	}
	h, err := hashPassword(password)
	if err != nil {
		return err
	}
	if err := a.st.SetPasswordHash(ctx, userID, h); err != nil {
		return err
	}
	return a.st.DeleteSessions(ctx, userID, keep)
}

// Users returns every account.
func (a *Accounts) Users(ctx context.Context) ([]domain.User, error) {
	if err := administering(ctx); err != nil {
		return nil, err
	}
	return a.st.ListUsers(ctx)
}

// User looks an account up by ID or name.
func (a *Accounts) User(ctx context.Context, ref string) (domain.User, error) {
	return a.st.GetUser(ctx, ref)
}

// LibraryName is the name of a user's library.
func (a *Accounts) LibraryName(ctx context.Context, u domain.User) (string, error) {
	lib, err := a.st.GetLibrary(ctx, u.LibraryID)
	return lib.Name, err
}

// UpdateProfile saves ctx's user's display name and email.
func (a *Accounts) UpdateProfile(ctx context.Context, display, email string) (domain.User, error) {
	u, ok := UserOf(ctx)
	if !ok {
		return u, unauthorized("sign in first")
	}
	u, err := a.st.GetUser(ctx, u.ID)
	if err != nil {
		return u, err
	}
	u.DisplayName, u.Email = strings.TrimSpace(display), strings.TrimSpace(email)
	return a.st.UpdateUser(ctx, u)
}

// SetRole makes a user an admin or a member; disabled stops them signing
// in, and ends their sessions. The last admin stays one.
func (a *Accounts) SetRole(ctx context.Context, ref string, role domain.Role, disabled bool) (domain.User, error) {
	if err := administering(ctx); err != nil {
		return domain.User{}, err
	}
	u, err := a.st.GetUser(ctx, ref)
	if err != nil {
		return u, err
	}
	if u.Admin() && !u.Disabled && (role != domain.RoleAdmin || disabled) {
		if err := a.otherAdmin(ctx, u.ID); err != nil {
			return u, err
		}
	}
	u.Role, u.Disabled = role, disabled
	if u, err = a.st.UpdateUser(ctx, u); err != nil {
		return u, err
	}
	if disabled {
		err = a.st.DeleteSessions(ctx, u.ID, "")
	}
	return u, err
}

// RemoveUser deletes an account and its library, with everything in it.
// The last admin can't be removed.
func (a *Accounts) RemoveUser(ctx context.Context, ref string) (domain.User, error) {
	if err := administering(ctx); err != nil {
		return domain.User{}, err
	}
	u, err := a.st.GetUser(ctx, ref)
	if err != nil {
		return u, err
	}
	if u.Admin() && !u.Disabled {
		if err := a.otherAdmin(ctx, u.ID); err != nil {
			return u, err
		}
	}
	return u, a.st.DeleteUser(ctx, u.ID)
}

func (a *Accounts) otherAdmin(ctx context.Context, except string) error {
	us, err := a.st.ListUsers(ctx)
	if err != nil {
		return err
	}
	for _, o := range us {
		if o.ID != except && o.Admin() && !o.Disabled {
			return nil
		}
	}
	return domain.Invalidf("this is the only admin; make another admin first")
}

// Invite makes an invitation code for one registration, by ctx's user (or
// the CLI). The code is shown once; only its hash is kept.
func (a *Accounts) Invite(ctx context.Context) (string, time.Time, error) {
	if err := administering(ctx); err != nil {
		return "", time.Time{}, err
	}
	code, err := newToken()
	if err != nil {
		return "", time.Time{}, err
	}
	code = code[:16]
	by := ""
	if u, ok := UserOf(ctx); ok {
		by = u.ID
	}
	now := time.Now().UTC()
	inv := domain.Invite{CodeHash: hashToken(code), CreatedBy: by, CreatedAt: now, ExpiresAt: now.Add(InviteTTL)}
	return code, inv.ExpiresAt, a.st.CreateInvite(ctx, inv)
}

// --- secrets ---

func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func hashToken(t string) string {
	h := sha256.Sum256([]byte(t))
	return hex.EncodeToString(h[:])
}

// Argon2id with OWASP's suggested minimum: 19 MiB, two passes, one lane.
const (
	argonMemory  = 19 * 1024
	argonTime    = 2
	argonThreads = 1
	argonKeyLen  = 32
)

// hashPassword returns a password's Argon2id hash in the usual encoded
// form, $argon2id$v=19$m=…,t=…,p=…$salt$key.
func hashPassword(p string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(p), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

// checkPassword reports whether p matches an encoded Argon2id hash, with
// the cost the hash was made with.
func checkPassword(encoded, p string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var version int
	var mem, passes uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false
	}
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &mem, &passes, &threads); err != nil {
		return false
	}
	salt, err1 := base64.RawStdEncoding.DecodeString(parts[4])
	key, err2 := base64.RawStdEncoding.DecodeString(parts[5])
	if err1 != nil || err2 != nil || len(key) == 0 {
		return false
	}
	got := argon2.IDKey([]byte(p), salt, passes, mem, threads, uint32(len(key)))
	return subtle.ConstantTimeCompare(got, key) == 1
}

// dummyHash is checked against when there's no such user, so a sign-in
// takes as long either way.
var dummyHash = sync.OnceValue(func() string {
	h, _ := hashPassword("no such user")
	return h
})
