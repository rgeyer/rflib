package main

import (
	"strings"
	"testing"
)

func TestOrgsSystemsAndPlans(t *testing.T) {
	c := newCLI(t)
	call := c.ok("channel", "list", "in:shared", "name:\"2m FM Call\"", "--fields", "id")
	c.ok("org", "add", "SBARC", "--callsign", "w6sba", "--reason", "pilot")
	c.fails("required flag(s) \"reason\" not set", "org", "add", "GRONK")

	// A shared channel's operator and AllStar node, found by query.
	c.ok("shared", "update", "--id", call, "--operators", "sbarc", "--networks", "allstar:43761", "--reason", "node")
	if got := c.ok("channel", "list", "in:shared org:SBARC allstar:43761", "--fields", "name,networks,operators"); got != "2m FM Call\tallstar:43761\tSBARC" {
		t.Errorf("org: and allstar: = %q", got)
	}
	c.fails("isn't one of the shared tags", "shared", "update", "--id", call, "--tags", "allstar", "--reason", "x")

	c.ok("system", "add", "SBARC Hub", "--link", "allstar", "--networks", "allstar:43763", "--operators", "SBARC", "--reason", "pilot")
	c.ok("system", "members", "add", "SBARC Hub", "--id", call, "--on-demand", "--reason", "pilot")
	if got := c.ok("channel", "list", `in:shared system:"SBARC Hub"`, "--fields", "name"); got != "2m FM Call" {
		t.Errorf("system: = %q", got)
	}
	if got := c.ok("system", "show", "sbarc hub"); !strings.Contains(got, "allstar:43763") || !strings.Contains(got, "2m FM Call\t146.520000\ton-demand") {
		t.Errorf("system show:\n%s", got)
	}
	if got := c.ok("org", "show", "SBARC"); !strings.Contains(got, "channels: 1") || !strings.Contains(got, "systems: 1") {
		t.Errorf("org show:\n%s", got)
	}
	c.fails("attached", "org", "delete", "SBARC", "--yes")

	// A plan, laid out as ICS 205, programmed onto a radio from memory 50.
	c.ok("plan", "add", "Fair", "--starts", "2026-07-15", "--ends", "2026-07-26", "--status", "published", "--reason", "pilot")
	c.ok("plan", "entries", "add", "Fair", "--id", call, "--name", "FAIR TAC1", "--assignment", "Tactical", "--reason", "pilot")
	c.ok("plan", "entries", "add", "Fair", "-q", "name:70cm mode:fm", "--section-start", "5", "--reason", "pilot")
	if got := c.ok("plan", "channels", "Fair", "--fields", "ch,name,assignment,rx"); got != "1\tFAIR TAC1\tTactical\t146.520000\n5\t70cm FM Call\t\t446.000000" {
		t.Errorf("plan channels:\n%s", got)
	}
	c.ok("radio", "add", "HT", "--model", "baofeng-uv5rm")
	c.ok("radio", "draft", "HT", "--start")
	c.ok("radio", "sections", "add", "HT", "Fair", "--start", "50")
	c.ok("radio", "sources", "add", "HT", "--plan", "fair", "--section", "fair", "--plan-numbers", "match")
	c.fails("not both", "radio", "sources", "add", "HT", "band:2m", "--plan", "fair")
	c.status("radio", "sync", "HT")
	if got := c.ok("radio", "channels", "list", "HT", "--fields", "memory,plan_ch,name"); got != "50\t1\tFAIR TAC1*\n54\t5\t70cm FM Call" {
		t.Errorf("radio channels:\n%s", got)
	}
	// Compact: the plan's order, without its gaps.
	c.ok("radio", "sources", "set", "HT", "1", "--plan-numbers", "compact")
	if got := c.ok("radio", "channels", "list", "HT", "--fields", "memory,plan_ch,name"); got != "50\t1\tFAIR TAC1*\n51\t5\t70cm FM Call" {
		t.Errorf("compact:\n%s", got)
	}
	c.ok("plan", "entries", "remove", "Fair", "2", "--reason", "smaller")
	c.status("radio", "sync", "HT")
	if got := c.ok("radio", "channels", "list", "HT", "--fields", "memory,name"); got != "50\tFAIR TAC1*" {
		t.Errorf("after the plan changed:\n%s", got)
	}
	if got := c.ok("plan", "history", "Fair"); !strings.Contains(got, "revision 4\t") || !strings.Contains(got, "smaller") {
		t.Errorf("history:\n%s", got)
	}
	c.ok("plan", "archive", "Fair", "--reason", "over")
	if got := c.ok("plan", "list", "--fields", "name,status,starts"); got != "Fair\tarchived\t2026-07-15" {
		t.Errorf("plan list = %q", got)
	}
	if got := c.status("radio", "sources", "remove", "HT", "1"); !strings.Contains(got, "and 1 channels") {
		t.Errorf("remove = %q", got)
	}

	// A shared backup restores its organizations, systems, and plans into
	// another database (whose shared list, seeded too, has the channels); a
	// dry run checks it all and leaves nothing behind.
	backup := writeFile(t, c.ok("shared", "export", "json"))
	restored := newCLI(t)
	if got := restored.status("shared", "import", "json", backup, "--reason", "restore", "--dry-run", "--on-duplicate", "update"); !strings.Contains(got, "Dry run, nothing written") {
		t.Errorf("dry run = %q", got)
	}
	if got := restored.ok("org", "list"); got != "" {
		t.Errorf("the dry run left organizations: %q", got)
	}
	restored.ok("shared", "import", "json", backup, "--reason", "restore", "--on-duplicate", "update")
	if got, want := restored.ok("plan", "channels", "Fair"), c.ok("plan", "channels", "Fair"); got != want {
		t.Errorf("restored plan:\n%s\nwant:\n%s", got, want)
	}
}
