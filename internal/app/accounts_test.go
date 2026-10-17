package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ryan/rflib/internal/domain"
	"github.com/ryan/rflib/internal/seed"
	"github.com/ryan/rflib/internal/store"
)

func TestFirstAccountIsTheAdminsAndTakesTheLibrary(t *testing.T) {
	s, d := newService(t)
	ctx := context.Background()
	add(t, s, "Before accounts", 146.52, domain.ServiceAmateur)
	a := NewAccounts(d)
	if _, err := a.AddUser(ctx, domain.User{Name: "n1call", Role: domain.RoleMember}, "a long password"); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("first account as a member = %v", err)
	}
	if _, err := a.Register(ctx, SignupOpen, domain.User{Name: "n1call"}, "a long password", ""); !errors.Is(err, ErrForbidden) {
		t.Fatalf("registering with no admin = %v", err)
	}
	admin, err := a.AddUser(ctx, domain.User{Name: "N0CALL", Role: domain.RoleAdmin}, "admin password")
	if err != nil {
		t.Fatal(err)
	}
	if admin.LibraryID != s.LibraryID() {
		t.Fatalf("the admin's library is %s, not the default %s", admin.LibraryID, s.LibraryID())
	}
	mine, err := Open(WithUser(ctx, admin), d, "")
	if err != nil {
		t.Fatal(err)
	}
	if cs, err := mine.ListChannels(ctx); err != nil || names(cs) != "Before accounts" {
		t.Fatalf("admin's channels = %s, %v", names(cs), err)
	}

	member, err := a.Register(ctx, SignupOpen, domain.User{Name: "N1CALL", Role: domain.RoleAdmin}, "a long password", "")
	if err != nil {
		t.Fatal(err)
	}
	if member.Role != domain.RoleMember || member.LibraryID == admin.LibraryID {
		t.Fatalf("member = %+v", member)
	}
	mctx := WithUser(ctx, member)
	theirs, err := Open(mctx, d, "")
	if err != nil {
		t.Fatal(err)
	}
	if cs, err := theirs.ListChannels(mctx); err != nil || len(cs) != 0 {
		t.Fatalf("a new member's library = %s, %v", names(cs), err)
	}
	if _, err := Open(mctx, d, domain.DefaultLibrary); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("a member opening the admin's library = %v", err)
	}
	if _, err := Open(WithUser(ctx, admin), d, member.LibraryID); err != nil {
		t.Fatalf("an admin opening a member's library = %v", err)
	}

	// Changes record who made them.
	c := add(t, theirs, "Theirs", 146.55, domain.ServiceAmateur)
	c.Notes = "edited"
	if _, _, err := theirs.UpdateChannel(mctx, c); err != nil {
		t.Fatal(err)
	}
	if _, revs, err := theirs.History(mctx, c.ID); err != nil || revs[len(revs)-1].Author != "N1CALL" {
		t.Fatalf("history = %+v, %v", revs, err)
	}
}

func TestMembersReadButDontChangeTheSharedList(t *testing.T) {
	s, d := newService(t)
	ctx := context.Background()
	a := NewAccounts(d)
	admin, err := a.AddUser(ctx, domain.User{Name: "n0call", Role: domain.RoleAdmin}, "admin password")
	if err != nil {
		t.Fatal(err)
	}
	member, err := a.Register(ctx, SignupOpen, domain.User{Name: "n1call"}, "a long password", "")
	if err != nil {
		t.Fatal(err)
	}
	sh, err := s.shared(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sh.UpdateSeed(WithUser(ctx, member), seedList(t, "us-noaa"), false); !errors.Is(err, ErrForbidden) {
		t.Fatalf("a member updating a built-in list = %v", err)
	}
	if _, err := sh.UpdateSeed(WithUser(ctx, admin), seedList(t, "us-noaa"), false); err != nil {
		t.Fatal(err)
	}
	mctx := WithUser(ctx, member)
	wx, err := sh.ListChannels(mctx)
	if err != nil || len(wx) == 0 {
		t.Fatalf("a member reading the shared list = %d, %v", len(wx), err)
	}
	wx[0].Notes = "mine"
	if _, _, err := sh.UpdateShared(mctx, wx[0], "test"); !errors.Is(err, ErrForbidden) {
		t.Errorf("UpdateShared = %v", err)
	}
	if _, err := sh.SetStatus(mctx, wx[0].ID, StatusChange{Status: domain.StatusOffAir}, "test"); !errors.Is(err, ErrForbidden) {
		t.Errorf("SetStatus = %v", err)
	}
	if _, err := sh.CreateOrg(mctx, domain.Organization{Name: "Mine", Kind: domain.OrgClub}, "test"); !errors.Is(err, ErrForbidden) {
		t.Errorf("CreateOrg = %v", err)
	}
	if _, err := sh.CreateOrg(WithUser(ctx, admin), domain.Organization{Name: "Club", Kind: domain.OrgClub}, "test"); err != nil {
		t.Errorf("an admin's CreateOrg = %v", err)
	}
	// A member's own library still annotates shared channels.
	theirs, err := Open(mctx, d, "")
	if err != nil {
		t.Fatal(err)
	}
	view, err := theirs.GetChannel(mctx, wx[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	view.Notes = "my notes"
	if _, _, err := theirs.UpdateChannel(mctx, view); err != nil {
		t.Errorf("annotating = %v", err)
	}
}

func seedList(t *testing.T, id string) seed.List {
	t.Helper()
	l, err := seed.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestAccountAdministration(t *testing.T) {
	_, d := newService(t)
	ctx := context.Background()
	a := NewAccounts(d)
	admin, err := a.AddUser(ctx, domain.User{Name: "n0call", Role: domain.RoleAdmin}, "admin password")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.SetRole(ctx, admin.Name, domain.RoleMember, false); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("demoting the only admin = %v", err)
	}
	if _, err := a.RemoveUser(ctx, admin.Name); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("removing the only admin = %v", err)
	}
	if err := domain.ValidatePassword("short"); err == nil {
		t.Error("a short password passed")
	}

	member, err := a.AddUser(ctx, domain.User{Name: "n1call", Role: domain.RoleMember}, "a long password")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Users(WithUser(ctx, member)); !errors.Is(err, ErrForbidden) {
		t.Errorf("a member listing accounts = %v", err)
	}
	_, token, err := a.SignIn(ctx, "N1CALL", "a long password")
	if err != nil {
		t.Fatal(err)
	}
	if u, err := a.SessionUser(ctx, token); err != nil || u.ID != member.ID {
		t.Fatalf("session = %+v, %v", u, err)
	}
	if _, _, err := a.SignIn(ctx, "n1call", "a wrong password"); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("wrong password = %v", err)
	}
	if _, _, err := a.SignIn(ctx, "nobody", "a long password"); !errors.Is(err, ErrUnauthorized) || !strings.Contains(err.Error(), "name or password") {
		t.Errorf("no such user = %v", err)
	}

	// Disabling ends the sessions and stops signing in.
	if _, err := a.SetRole(ctx, member.Name, domain.RoleMember, true); err != nil {
		t.Fatal(err)
	}
	if _, err := a.SessionUser(ctx, token); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("a disabled user's session = %v", err)
	}
	if _, _, err := a.SignIn(ctx, "n1call", "a long password"); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("a disabled user signing in = %v", err)
	}
	if _, err := a.SetRole(ctx, member.Name, domain.RoleMember, false); err != nil {
		t.Fatal(err)
	}

	// An admin's reset signs the user out everywhere.
	_, token, err = a.SignIn(ctx, "n1call", "a long password")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.SetPassword(ctx, "n1call", "a new long password"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.SessionUser(ctx, token); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("session after a reset = %v", err)
	}
	if _, _, err := a.SignIn(ctx, "n1call", "a new long password"); err != nil {
		t.Errorf("signing in with the new password = %v", err)
	}

	// Removing an account deletes its library.
	if _, err := a.RemoveUser(ctx, "n1call"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.GetLibrary(ctx, member.LibraryID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("the removed user's library = %v", err)
	}
}

func TestInvitationsAreSpentOnce(t *testing.T) {
	_, d := newService(t)
	ctx := context.Background()
	a := NewAccounts(d)
	if _, err := a.AddUser(ctx, domain.User{Name: "n0call", Role: domain.RoleAdmin}, "admin password"); err != nil {
		t.Fatal(err)
	}
	code, _, err := a.Invite(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// A registration that fails doesn't spend the code.
	if _, err := a.Register(ctx, SignupInvite, domain.User{Name: "n0call"}, "a long password", code); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("taken name = %v", err)
	}
	if _, err := a.Register(ctx, SignupInvite, domain.User{Name: "n1call"}, "a long password", code); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Register(ctx, SignupInvite, domain.User{Name: "n2call"}, "a long password", code); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("a spent code = %v", err)
	}
	if _, err := a.Register(ctx, SignupClosed, domain.User{Name: "n2call"}, "a long password", ""); !errors.Is(err, ErrForbidden) {
		t.Errorf("closed sign-up = %v", err)
	}
}

func TestPasswordHashes(t *testing.T) {
	h, err := hashPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Errorf("hash = %s", h)
	}
	if !checkPassword(h, "correct horse") || checkPassword(h, "correct horsf") || checkPassword("garbage", "correct horse") {
		t.Error("checkPassword")
	}
	if h2, _ := hashPassword("correct horse"); h2 == h {
		t.Error("two hashes of one password are the same; the salt isn't random")
	}
}

func TestCallsignForm(t *testing.T) {
	for name, want := range map[string]bool{"N0CALL": true, "n1call": true, "W1CALL": true, "VE3CALL": true, "2E0CALL": true, "XXXX000": true, "pat": false, "radio-fan": false} {
		if got := domain.IsCallsign(name); got != want {
			t.Errorf("IsCallsign(%q) = %v", name, got)
		}
	}
}
