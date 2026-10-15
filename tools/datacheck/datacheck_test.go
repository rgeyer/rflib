package main

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ryan/rflib/internal/model"
	"github.com/ryan/rflib/internal/seed"
)

func testdata(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// Every built-in list and model is covered by a dataset, so new data can't
// skip maintenance.
func TestEveryListAndModelIsCovered(t *testing.T) {
	m, err := loadManifest("sources.yaml")
	if err != nil {
		t.Fatal(err)
	}
	covered := map[string]string{}
	for _, d := range m.Datasets {
		for _, c := range d.Covers {
			if other, ok := covered[c]; ok {
				t.Errorf("%s is covered by %s and %s", c, other, d.ID)
			}
			covered[c] = d.ID
		}
	}
	var want []string
	for _, l := range seed.Lists() {
		want = append(want, l.ID)
	}
	models, err := model.Builtins()
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range models {
		want = append(want, m.Name)
	}
	for _, id := range want {
		if covered[id] == "" {
			t.Errorf("%s isn't covered by any dataset in sources.yaml", id)
		}
	}
	for c := range covered {
		if !slices.Contains(want, c) && c != "band-plan" {
			t.Errorf("sources.yaml covers %s, which isn't a built-in list or model", c)
		}
	}
}

func TestParseFrequencyList(t *testing.T) {
	src := testdata(t, "wsjtx.cpp")
	ft4 := parseFrequencyList(src, "FT4")
	if len(ft4) == 0 || ft4[0] != 3_575_000 || slices.Contains(ft4, 3_568_000) {
		t.Errorf("FT4 = %v (region 3's 3.568 must be left out)", ft4)
	}
	if ft8 := parseFrequencyList(src, "FT8"); !slices.Contains(ft8, 14_074_000) || slices.Contains(ft8, 70_154_000) {
		t.Errorf("FT8 = %v (region 1's 4 m must be left out)", ft8)
	}
}

func TestParseAMSAT(t *testing.T) {
	sats := parseAMSAT(testdata(t, "amsat.html"))
	if len(sats) != 3 {
		t.Fatalf("satellites = %+v", sats)
	}
	if s := sats[0]; s.Name != "AO-91" || s.UplinkHz != 435_250_000 || s.DownHz != 145_960_000 || s.ToneHz != "" {
		t.Errorf("AO-91 = %+v", s)
	}
	if s := sats[1]; s.Name != "AO-123" || s.ToneHz != "67" {
		t.Errorf("AO-123 = %+v", s)
	}
}

func TestParseRadiofax(t *testing.T) {
	call, hz := parseRadiofax(testdata(t, "hfgulf.txt"))
	if call != "NMG" || !slices.Equal(hz, []int64{4_317_900, 8_503_900, 12_789_900, 17_146_400}) {
		t.Errorf("NMG = %s %v", call, hz)
	}
}

// fakeFetch serves fixed responses by URL; others are gone.
func fakeFetch(pages map[string]string) fetchFunc {
	return func(_ context.Context, url string) ([]byte, error) {
		if p, ok := pages[url]; ok {
			return []byte(p), nil
		}
		return nil, fmt.Errorf("%w (HTTP 404): %s", errGone, url)
	}
}

func TestChecks(t *testing.T) {
	now := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	ecfr := `{"content_versions": [
		{"identifier": "97.301", "name": "§ 97.301   Authorized frequency bands.", "amendment_date": "2024-01-08"},
		{"identifier": "97.301", "name": "§ 97.301   Authorized frequency bands.", "amendment_date": "2026-02-13"},
		{"identifier": "97.305", "name": "§ 97.305   Authorized emission types.", "amendment_date": "2026-02-13"}]}`
	c := &checker{now: now, dir: ".", fetch: fakeFetch(map[string]string{
		"wsjtx":  "{3575000, Modes::FT4, IARURegions::ALL}, {7047500, Modes::FT4, IARURegions::ALL}, {7099000, Modes::FT4, IARURegions::R2}",
		"broken": "nothing here",
		"https://www.ecfr.gov/api/versioner/v1/versions/title-47.json?part=97": ecfr,
	})}
	m := Manifest{Datasets: []Dataset{
		{ID: "ft4", Covers: []string{"us-ft4"}, Check: "extract", Kind: "wsjtx", Mode: "FT4", Source: "wsjtx", Ignore: []string{"14.08"}},
		{ID: "broken", Covers: []string{"us-ft4"}, Check: "extract", Kind: "wsjtx", Mode: "FT4", Source: "broken"},
		{ID: "gone", Covers: []string{"us-js8"}, Check: "extract", Kind: "js8", Source: "elsewhere"},
		{ID: "part97", Covers: []string{"us-60m"}, Check: "watch", Kind: "ecfr", Title: 47, Part: 97, Recorded: map[string]string{"97.301": "2024-01-08", "97.305": "2026-02-13"}},
		{ID: "old", Covers: []string{"us-sstv"}, Check: "review", Verified: "2025-01-01", Every: "365d"},
		{ID: "fresh", Covers: []string{"us-psk31"}, Check: "review", Verified: "2026-10-01", Every: "365d"},
		{ID: "models", Covers: []string{"yaesu-ft857"}, Check: "extract", Kind: "chirp-models"},
	}}
	got := map[string]Result{}
	for _, r := range c.runAll(context.Background(), m, nil) {
		got[r.ID] = r
	}
	ft4 := strings.Join(got["ft4"].Findings, "\n")
	if !strings.Contains(ft4, "WSJT-X lists FT4 on 7.099 MHz, which us-ft4 doesn't have") || !strings.Contains(ft4, "FT4 10m (28.18 MHz), which WSJT-X no longer lists") || strings.Contains(ft4, "14.08 MHz") {
		t.Errorf("ft4 findings:\n%s", ft4)
	}
	if r := got["broken"]; r.Status != "error" || !strings.Contains(r.Error, "format changed") {
		t.Errorf("broken = %+v", r)
	}
	if r := got["gone"]; r.Status != "findings" || !strings.Contains(r.Findings[0], "the source is gone") {
		t.Errorf("gone = %+v", r)
	}
	if r := got["part97"]; len(r.Findings) != 1 || r.Findings[0] != "§ 97.301 Authorized frequency bands. was amended 2026-02-13; rflib's data was reviewed against the 2024-01-08 version." {
		t.Errorf("part97 = %+v", r)
	}
	if got["old"].Status != "findings" || got["fresh"].Status != "ok" {
		t.Errorf("reviews: old %+v, fresh %+v", got["old"], got["fresh"])
	}
	if got["models"].Status != "skipped" {
		t.Errorf("CHIRP check without --chirp = %+v", got["models"])
	}
}

func TestChirpChecks(t *testing.T) {
	ft857, err := model.Find("yaesu-ft857")
	if err != nil {
		t.Fatal(err)
	}
	r := chirpRadio{Driver: "ft857.FT857USRadio", BandsMHz: [][2]float64{{420, 470}, {0.1, 33}, {33, 56}, {76, 108}, {108, 136.999999}, {137, 164}}, Modes: ft857.Modes}
	r.Memories.First, r.Memories.Last, r.Names.MaxLength = 1, 100, 8
	c := &checker{dir: t.TempDir(), chirp: &chirpDump{Radios: map[string]chirpRadio{ft857.ChirpModel: r, "Acme X-1": {Driver: "acme.X1"}}}}
	findings, err := c.chirpModels(Dataset{Covers: []string{"yaesu-ft857"}})
	if err != nil || len(findings) != 1 || !strings.Contains(findings[0], "memories is 1-200; CHIRP's driver (ft857.FT857USRadio) says 1-100") {
		t.Errorf("model drift = %q, %v", findings, err)
	}
	if err := os.WriteFile(c.dir+"/radios.txt", []byte(ft857.ChirpModel+"\nGone G-1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	findings, err = c.chirpRadios(Dataset{Baseline: "radios.txt"})
	if err != nil || len(findings) != 3 || !strings.HasPrefix(findings[0], "New in CHIRP: Acme X-1") || findings[1] != "No longer in CHIRP: Gone G-1." {
		t.Errorf("new radios = %q, %v", findings, err)
	}
}

func TestSyncIssues(t *testing.T) {
	var calls []string
	gh := func(args ...string) ([]byte, error) {
		calls = append(calls, strings.Join(args[:2], " ")+" "+args[2])
		if args[0] == "issue" && args[1] == "list" {
			return []byte(`[{"number": 7, "title": "Data check: us-ft4"}]`), nil
		}
		return nil, nil
	}
	results := []Result{
		{ID: "us-ft4", Status: "findings", Findings: []string{"a"}},
		{ID: "us-js8", Status: "findings", Findings: []string{"b"}},
		{ID: "us-ft8", Status: "ok"},
	}
	if err := syncIssues(results, time.Now(), gh); err != nil {
		t.Fatal(err)
	}
	want := []string{"label create data-check", "issue list --label", "issue edit 7", "issue create --title"}
	if !slices.Equal(calls, want) {
		t.Errorf("gh calls = %q", calls)
	}
}

func TestManifestValidation(t *testing.T) {
	for _, tc := range []struct {
		d    Dataset
		want string
	}{
		{Dataset{ID: "x", Covers: []string{"y"}, Check: "extract", Kind: "ecfr"}, "doesn't go with check extract"},
		{Dataset{ID: "x", Covers: []string{"y"}, Check: "extract", Kind: "wsjtx"}, "needs a mode"},
		{Dataset{ID: "x", Covers: []string{"y"}, Check: "review", Verified: "soon", Every: "365d"}, "YYYY-MM-DD"},
		{Dataset{ID: "x", Covers: []string{"y"}, Check: "review", Verified: "2026-01-01", Every: "1y"}, "number of days"},
	} {
		if err := tc.d.validate(); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%+v: err = %v, want %q", tc.d, err, tc.want)
		}
	}
}
