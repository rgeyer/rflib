package main

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Issues carry this label and a fixed title per dataset, so each run
// updates the same issue instead of opening another.
const issueLabel = "data-check"

func issueTitle(id string) string { return "Data check: " + id }

func issueBody(r Result, now time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "The weekly data check found differences between **%s** and its source.\n\n", r.ID)
	fmt.Fprintf(&b, "- **Covers:** %s\n- **Check:** %s\n- **Source:** %s\n\n", strings.Join(r.Covers, ", "), r.Check, r.Source)
	b.WriteString("Findings:\n\n")
	for _, f := range r.Findings {
		fmt.Fprintf(&b, "- %s\n", f)
	}
	b.WriteString("\nTo resolve: update the data, or `tools/datacheck/sources.yaml` (ignore, recorded, or verified) for a difference that's accepted, and close this issue (\"Fixes #N\" in the commit does it on merge). ")
	b.WriteString("An agent can help: run `/maintain-data` in Claude Code.\n\n")
	fmt.Fprintf(&b, "_Last checked %s._\n", now.Format(time.DateOnly))
	return b.String()
}

// ghFunc runs gh with args and returns its stdout.
type ghFunc func(args ...string) ([]byte, error)

func ghRun(args ...string) ([]byte, error) {
	out, err := exec.Command("gh", args...).Output()
	if ee, ok := err.(*exec.ExitError); ok {
		return out, fmt.Errorf("gh %s: %s", args[0], strings.TrimSpace(string(ee.Stderr)))
	}
	return out, err
}

// syncIssues opens or updates an issue for each dataset with findings.
// Issues for datasets that came back clean are left for a person to close.
func syncIssues(results []Result, now time.Time, gh ghFunc) error {
	if _, err := gh("label", "create", issueLabel, "--color", "c5def5", "--description", "Shipped data differs from its source", "--force"); err != nil {
		return err
	}
	out, err := gh("issue", "list", "--label", issueLabel, "--state", "open", "--limit", "200", "--json", "number,title")
	if err != nil {
		return err
	}
	var open []struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
	}
	if err := json.Unmarshal(out, &open); err != nil {
		return fmt.Errorf("read gh issue list: %w", err)
	}
	byTitle := map[string]int{}
	for _, i := range open {
		byTitle[i.Title] = i.Number
	}
	for _, r := range results {
		if r.Status != "findings" {
			continue
		}
		title, body := issueTitle(r.ID), issueBody(r, now)
		if n, ok := byTitle[title]; ok {
			_, err = gh("issue", "edit", fmt.Sprint(n), "--body", body)
		} else {
			_, err = gh("issue", "create", "--title", title, "--label", issueLabel, "--body", body)
		}
		if err != nil {
			return err
		}
	}
	return nil
}
