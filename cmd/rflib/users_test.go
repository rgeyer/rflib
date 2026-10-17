package main

import (
	"strings"
	"testing"
)

func TestUserCommands(t *testing.T) {
	c := newCLI(t)
	c.fails("--accounts", "serve", "--signup", "open")
	c.fails("no admin", "serve", "--accounts")
	c.fails("needs HTTPS", "serve", "--accounts", "--addr", "0.0.0.0:8080")
	c.fails("--admin", "user", "add", "n1call")
	if _, _, err := c.execIn("short\n", "user", "add", "n0call", "--admin"); err == nil || !strings.Contains(err.Error(), "at least 10") {
		t.Fatalf("short password: %v", err)
	}
	if _, errOut, err := c.execIn("admin password\n", "user", "add", "N0CALL", "--admin", "--display", "Sam"); err != nil || !strings.Contains(errOut, "with the library default") {
		t.Fatalf("add admin: %v %s", err, errOut)
	}
	if _, errOut, err := c.execIn("a long password\n", "user", "add", "n1call"); err != nil || !strings.Contains(errOut, "with the library n1call") {
		t.Fatalf("add member: %v %s", err, errOut)
	}
	list := c.ok("user", "list", "--fields", "name,display,role,status,callsign")
	if !strings.Contains(list, "N0CALL") || !strings.Contains(list, "admin") || !strings.Contains(list, "yes (unverified)") || !strings.Contains(list, "n1call") {
		t.Errorf("user list:\n%s", list)
	}
	c.fails("only admin", "user", "role", "n0call", "--member")
	if got := c.status("user", "role", "n1call", "--disable"); got != "n1call is member, disabled" {
		t.Errorf("disable: %s", got)
	}
	if _, _, err := c.execIn("a new long password\n", "user", "passwd", "n1call"); err != nil {
		t.Fatal(err)
	}
	code := c.ok("user", "invite")
	if len(code) != 16 {
		t.Errorf("invite code %q", code)
	}
	c.fails("--yes", "user", "remove", "n1call")
	c.fails("only admin", "user", "remove", "n0call", "--yes")
	if got := c.status("user", "remove", "n1call", "--yes"); got != "Removed n1call and their library" {
		t.Errorf("remove: %s", got)
	}
	// The CLI keeps working on the admin's library, the default.
	c.ok("channel", "list")
}
