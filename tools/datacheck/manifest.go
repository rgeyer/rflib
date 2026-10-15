package main

import (
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Manifest is sources.yaml: where each piece of shipped data comes from.
type Manifest struct {
	Datasets []Dataset `yaml:"datasets"`
}

// Dataset is one checked source and the built-in lists and models it
// covers.
type Dataset struct {
	ID     string   `yaml:"id"`
	Covers []string `yaml:"covers"`
	// Check is extract, watch, or review; Kind picks the parser.
	Check   string   `yaml:"check"`
	Kind    string   `yaml:"kind"`
	Source  string   `yaml:"source"`
	Sources []string `yaml:"sources"`
	// Mode is the WSJT-X mode, for kind wsjtx.
	Mode string `yaml:"mode"`
	// Ignore lists differences that are known and accepted: frequencies
	// in MHz, or names.
	Ignore []string `yaml:"ignore"`
	// Title and Part select the eCFR rules, for kind ecfr; Recorded maps
	// each section to the amendment date the data was reviewed against.
	Title    int               `yaml:"title"`
	Part     int               `yaml:"part"`
	Recorded map[string]string `yaml:"recorded"`
	// Baseline is a file, beside the manifest, for kind chirp-radios.
	Baseline string `yaml:"baseline"`
	// Verified and Every drive review reminders.
	Verified string `yaml:"verified"`
	Every    string `yaml:"every"`
}

func loadManifest(path string) (Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}
	var m Manifest
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(&m); err != nil {
		return m, fmt.Errorf("%s: %w", path, err)
	}
	seen := map[string]bool{}
	for _, d := range m.Datasets {
		if err := d.validate(); err != nil {
			return m, fmt.Errorf("%s: dataset %q: %w", path, d.ID, err)
		}
		if seen[d.ID] {
			return m, fmt.Errorf("%s: dataset %q is listed twice", path, d.ID)
		}
		seen[d.ID] = true
	}
	return m, nil
}

var kinds = map[string][]string{
	"extract": {"wsjtx", "js8", "amsat-fm", "nws-radiofax", "chirp-models", "chirp-radios"},
	"watch":   {"ecfr"},
	"review":  {""},
}

func (d Dataset) validate() error {
	if d.ID == "" || len(d.Covers) == 0 {
		return fmt.Errorf("id and covers are required")
	}
	ks, ok := kinds[d.Check]
	if !ok {
		return fmt.Errorf("check must be extract, watch, or review")
	}
	if !slices.Contains(ks, d.Kind) {
		return fmt.Errorf("kind %q doesn't go with check %s", d.Kind, d.Check)
	}
	switch {
	case d.Kind == "wsjtx" && d.Mode == "":
		return fmt.Errorf("kind wsjtx needs a mode")
	case d.Kind == "ecfr" && (d.Title == 0 || d.Part == 0 || len(d.Recorded) == 0):
		return fmt.Errorf("kind ecfr needs title, part, and recorded sections")
	case d.Kind == "chirp-radios" && d.Baseline == "":
		return fmt.Errorf("kind chirp-radios needs a baseline")
	case d.Check == "review":
		if _, err := time.Parse(time.DateOnly, d.Verified); err != nil {
			return fmt.Errorf("verified must be a YYYY-MM-DD date")
		}
		if _, err := parseDays(d.Every); err != nil {
			return err
		}
	}
	return nil
}

// parseDays reads an interval such as "365d".
func parseDays(s string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
	if err != nil || !strings.HasSuffix(s, "d") || n <= 0 {
		return 0, fmt.Errorf("every must be a number of days, such as 365d")
	}
	return n, nil
}

// ignored reports whether a finding's key is in the dataset's ignore list.
func (d Dataset) ignored(key string) bool { return slices.Contains(d.Ignore, key) }
