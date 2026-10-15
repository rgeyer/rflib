// Command datacheck checks rflib's shipped data (built-in lists, radio
// models, and the rules behind band plans) against its sources, as listed
// in sources.yaml. It prints a report and, with -issues, opens or updates
// one GitHub issue per dataset with findings.
//
//	uv run tools/chirp_model.py --json > chirp.json
//	go run ./tools/datacheck -chirp chirp.json
//
// Without -chirp, the CHIRP checks are skipped. A dataset that can't be
// checked (a fetch fails, or a source's format changed) is an error: the
// run exits 1 after reporting everything else.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Result is one dataset's outcome.
type Result struct {
	ID       string   `json:"id"`
	Covers   []string `json:"covers"`
	Check    string   `json:"check"`
	Source   string   `json:"source"`
	Status   string   `json:"status"` // ok, findings, skipped, or error
	Findings []string `json:"findings,omitempty"`
	Error    string   `json:"error,omitempty"`
}

func main() {
	manifest := flag.String("manifest", "tools/datacheck/sources.yaml", "The sources manifest")
	chirpPath := flag.String("chirp", "", "Output of `uv run tools/chirp_model.py --json`; without it, CHIRP checks are skipped")
	only := flag.String("only", "", "Comma-separated dataset IDs to check (default all)")
	jsonOut := flag.String("json", "", "Also write the results as JSON to this file")
	summary := flag.String("summary", "", "Append the Markdown report to this file (such as $GITHUB_STEP_SUMMARY) instead of stdout")
	issues := flag.Bool("issues", false, "Open or update a GitHub issue per dataset with findings (uses gh)")
	flag.Parse()
	if err := run(*manifest, *chirpPath, *only, *jsonOut, *summary, *issues); err != nil {
		fmt.Fprintln(os.Stderr, "datacheck:", err)
		os.Exit(1)
	}
}

func run(manifestPath, chirpPath, only, jsonOut, summary string, issues bool) error {
	m, err := loadManifest(manifestPath)
	if err != nil {
		return err
	}
	c := &checker{fetch: httpFetch, now: time.Now(), dir: filepath.Dir(manifestPath)}
	if chirpPath != "" {
		if c.chirp, err = loadChirp(chirpPath); err != nil {
			return err
		}
	}
	var ids []string
	if only != "" {
		ids = strings.Split(only, ",")
	}
	results := c.runAll(context.Background(), m, ids)

	if summary == "" {
		writeMarkdown(os.Stdout, results, c.now)
	} else if err := appendMarkdown(summary, results, c.now); err != nil {
		return err
	}
	if jsonOut != "" {
		data, _ := json.MarshalIndent(results, "", "  ")
		if err := os.WriteFile(jsonOut, append(data, '\n'), 0o644); err != nil {
			return err
		}
	}
	if issues {
		if err := syncIssues(results, c.now, ghRun); err != nil {
			return err
		}
	}
	var failed []string
	for _, r := range results {
		if r.Status == "error" {
			failed = append(failed, r.ID)
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("couldn't check %s", strings.Join(failed, ", "))
	}
	return nil
}

func (c *checker) runAll(ctx context.Context, m Manifest, ids []string) []Result {
	var results []Result
	for _, d := range m.Datasets {
		if len(ids) > 0 && !slices.Contains(ids, d.ID) {
			continue
		}
		r := Result{ID: d.ID, Covers: d.Covers, Check: d.Check, Source: d.Source}
		if r.Source == "" {
			r.Source = strings.Join(d.Sources, ", ")
		}
		findings, err := c.check(ctx, d)
		switch {
		case errors.Is(err, errSkipped):
			r.Status = "skipped"
		case errors.Is(err, errGone):
			r.Status, r.Findings = "findings", []string{err.Error() + "; the data may need to go too."}
		case err != nil:
			r.Status, r.Error = "error", err.Error()
		case len(findings) > 0:
			r.Status, r.Findings = "findings", findings
		default:
			r.Status = "ok"
		}
		results = append(results, r)
	}
	return results
}

func appendMarkdown(path string, results []Result, now time.Time) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	writeMarkdown(f, results, now)
	return f.Close()
}

func writeMarkdown(w io.Writer, results []Result, now time.Time) {
	fmt.Fprintf(w, "## Data check, %s\n\n", now.Format(time.DateOnly))
	fmt.Fprintln(w, "| Dataset | Check | Status |")
	fmt.Fprintln(w, "|---|---|---|")
	for _, r := range results {
		status := r.Status
		if r.Status == "findings" {
			status = fmt.Sprintf("%d findings", len(r.Findings))
		}
		fmt.Fprintf(w, "| %s | %s | %s |\n", r.ID, r.Check, status)
	}
	for _, r := range results {
		if r.Status != "findings" && r.Status != "error" {
			continue
		}
		fmt.Fprintf(w, "\n### %s\n\n", r.ID)
		if r.Error != "" {
			fmt.Fprintf(w, "Couldn't check: %s\n", r.Error)
		}
		for _, f := range r.Findings {
			fmt.Fprintf(w, "- %s\n", f)
		}
	}
}
