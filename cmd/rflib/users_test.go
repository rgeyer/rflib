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
	c.fails("--admin", "user", "add", "kd2abc")
	if _, _, err := c.execIn("short\n", "user", "add", "k7adm", "--admin"); err == nil || !strings.Contains(err.Error(), "at least 10") {
		t.Fatalf("short password: %v", err)
	}
	if _, errOut, err := c.execIn("admin password\n", "user", "add", "K7ADM", "--admin", "--display", "Sam"); err != nil || !strings.Contains(errOut, "with the library default") {
		t.Fatalf("add admin: %v %s", err, errOut)
	}
	if _, errOut, err := c.execIn("a long password\n", "user", "add", "kd2abc"); err != nil || !strings.Contains(errOut, "with the library kd2abc") {
		t.Fatalf("add member: %v %s", err, errOut)
	}
	list := c.ok("user", "list", "--fields", "name,display,role,status,callsign")
	if !strings.Contains(list, "K7ADM") || !strings.Contains(list, "admin") || !strings.Contains(list, "yes (unverified)") || !strings.Contains(list, "kd2abc") {
		t.Errorf("user list:\n%s", list)
	}
	c.fails("only admin", "user", "role", "k7adm", "--member")
	if got := c.status("user", "role", "kd2abc", "--disable"); got != "kd2abc is member, disabled" {
		t.Errorf("disable: %s", got)
	}
	if _, _, err := c.execIn("a new long password\n", "user", "passwd", "kd2abc"); err != nil {
		t.Fatal(err)
	}
	code := c.ok("user", "invite")
	if len(code) != 16 {
		t.Errorf("invite code %q", code)
	}
	c.fails("--yes", "user", "remove", "kd2abc")
	c.fails("only admin", "user", "remove", "k7adm", "--yes")
	if got := c.status("user", "remove", "kd2abc", "--yes"); got != "Removed kd2abc and their library" {
		t.Errorf("remove: %s", got)
	}
	// The CLI keeps working on the admin's library, the default.
	c.ok("channel", "list")
}
