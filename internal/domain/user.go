package domain

import (
	"regexp"
	"strings"
	"time"
)

// A user is someone with an account on a server that has accounts (rflib
// serve --accounts). Each user has one private library, made with the
// account, and signs in with a method recorded as an identity: a local
// password today, others later. Local mode (no accounts) has no users.

// Role is what a user may do beyond their own library.
type Role string

const (
	// RoleMember uses their own library and reads the shared list.
	RoleMember Role = "member"
	// RoleAdmin also maintains the shared list (its channels,
	// organizations, systems, plans, and built-in lists) and the accounts.
	RoleAdmin Role = "admin"
)

// User is an account.
type User struct {
	ID string
	// Name is what the user signs in with: a callsign or a username,
	// unique ignoring case.
	Name        string
	DisplayName string
	Email       string
	Role        Role
	// LibraryID is the user's private library.
	LibraryID string
	// Disabled users can't sign in.
	Disabled  bool
	CreatedAt time.Time
}

// Admin reports whether the user is an admin.
func (u User) Admin() bool { return u.Role == RoleAdmin }

// Callsign reports whether the user's name has a callsign's form. It's
// unverified: the form says nothing of whether the user holds it.
func (u User) Callsign() bool { return IsCallsign(u.Name) }

// Label is the user's display name, or their name.
func (u User) Label() string {
	if u.DisplayName != "" {
		return u.DisplayName
	}
	return u.Name
}

var (
	userName = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{2,31}$`)
	// amateur callsigns (a prefix of one or two characters, a digit, and a
	// suffix of letters) and GMRS ones (four letters, three digits).
	callsign = regexp.MustCompile(`^(?:(?:[A-Z]{1,2}|[A-Z][0-9]|[0-9][A-Z])[0-9][A-Z]{1,4}|[A-Z]{4}[0-9]{3})$`)
)

// IsCallsign reports whether s has a callsign's form, ignoring case.
func IsCallsign(s string) bool { return callsign.MatchString(strings.ToUpper(s)) }

// reservedNames can't be user names: a user's library takes their name, and
// these are libraries' own names.
var reservedNames = map[string]bool{SharedLibrary: true, DefaultLibrary: true, "admin": true}

// ValidateUser checks a user's name, display name, email, and role.
func ValidateUser(u User) error {
	n := strings.ToLower(u.Name)
	if !userName.MatchString(n) {
		return Invalidf("a name is a callsign or a username: 3 to 32 letters, digits, dots, dashes, or underscores, starting with a letter or digit")
	}
	if reservedNames[n] {
		return Invalidf("%q is reserved; choose another name", u.Name)
	}
	if len(u.DisplayName) > 64 || u.DisplayName != strings.TrimSpace(u.DisplayName) {
		return Invalidf("a display name is at most 64 characters, without leading or trailing spaces")
	}
	if u.Email != "" && (len(u.Email) > 254 || !strings.Contains(u.Email, "@") || strings.ContainsAny(u.Email, " \t\r\n")) {
		return Invalidf("%q isn't an email address", u.Email)
	}
	if u.Role != RoleMember && u.Role != RoleAdmin {
		return Invalidf("a role is member or admin")
	}
	return nil
}

// ValidatePassword checks a new password's length.
func ValidatePassword(p string) error {
	if len(p) < 10 {
		return Invalidf("a password needs at least 10 characters")
	}
	if len(p) > 256 {
		return Invalidf("a password is at most 256 characters")
	}
	return nil
}

// Session is a signed-in browser. Its token is given to the browser once;
// only a hash of it is kept.
type Session struct {
	TokenHash string
	UserID    string
	CreatedAt time.Time
	ExpiresAt time.Time
}

// Invite lets one person register while sign-up is by invitation. Its code
// is shown once; only a hash of it is kept.
type Invite struct {
	CodeHash  string
	CreatedBy string
	CreatedAt time.Time
	ExpiresAt time.Time
	UsedBy    string
	UsedAt    *time.Time
}
