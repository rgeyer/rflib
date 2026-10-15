package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ryan/rflib/internal/domain"
	"github.com/ryan/rflib/internal/model"
	"github.com/ryan/rflib/internal/seed"
)

// errGone marks a source that no longer exists (HTTP 404 or 410): a
// finding, since the data may need to go too, not a failed run.
var errGone = errors.New("the source is gone")

type fetchFunc func(ctx context.Context, url string) ([]byte, error)

func httpFetch(ctx context.Context, url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "rflib-datacheck (+https://github.com/rgeyer/rflib)")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }() // read-only
	switch {
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		return nil, fmt.Errorf("%w (HTTP %d): %s", errGone, resp.StatusCode, url)
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("fetch %s: HTTP %d", url, resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// checker runs datasets' checks. Chirp is the output of
// `tools/chirp_model.py --json`, or nil to skip the CHIRP checks.
type checker struct {
	fetch fetchFunc
	now   time.Time
	dir   string // the manifest's directory, for baselines
	chirp *chirpDump
}

// errSkipped marks a check that couldn't run here, such as the CHIRP
// checks without --chirp.
var errSkipped = errors.New("skipped")

func (c *checker) check(ctx context.Context, d Dataset) ([]string, error) {
	switch d.Kind {
	case "wsjtx":
		return c.dialFrequencies(ctx, d, d.Mode, "WSJT-X")
	case "js8":
		return c.dialFrequencies(ctx, d, "JS8", "JS8Call")
	case "amsat-fm":
		return c.amsatFM(ctx, d)
	case "nws-radiofax":
		return c.radiofax(ctx, d)
	case "ecfr":
		return c.ecfr(ctx, d)
	case "chirp-models":
		return c.chirpModels(d)
	case "chirp-radios":
		return c.chirpRadios(d)
	}
	return c.review(d), nil
}

func (c *checker) review(d Dataset) []string {
	verified, _ := time.Parse(time.DateOnly, d.Verified) // checked by validate
	days, _ := parseDays(d.Every)
	if c.now.Sub(verified) < time.Duration(days)*24*time.Hour {
		return nil
	}
	return []string{fmt.Sprintf("Last verified %s; due for review every %d days. Check against the source, then set verified in sources.yaml.", d.Verified, days)}
}

// listHz returns a built-in list's channels by receive frequency.
func listHz(id string) (map[int64]domain.Channel, error) {
	l, err := seed.Get(id)
	if err != nil {
		return nil, err
	}
	b, err := l.Batch()
	if err != nil {
		return nil, err
	}
	out := map[int64]domain.Channel{}
	for _, r := range b.Records {
		out[r.Channel.ReceiveHz] = r.Channel
	}
	return out, nil
}

func mhz(hz int64) string { return strconv.FormatFloat(float64(hz)/1e6, 'f', -1, 64) }

// --- WSJT-X and JS8Call ---

// frequencyEntry matches the default list's entries, such as
// {14074000, Modes::FT8, IARURegions::ALL, ...}.
var frequencyEntry = regexp.MustCompile(`\{\s*(\d+)\s*,\s*Modes::(\w+)\s*,\s*IARURegions::(\w+)`)

// parseFrequencyList returns the dial frequencies a WSJT-X-style list
// gives a mode in the US (region 2, or all regions).
func parseFrequencyList(src []byte, mode string) []int64 {
	var out []int64
	for _, m := range frequencyEntry.FindAllSubmatch(src, -1) {
		if string(m[2]) != mode || (string(m[3]) != "ALL" && string(m[3]) != "R2") {
			continue
		}
		hz, _ := strconv.ParseInt(string(m[1]), 10, 64)
		if !slices.Contains(out, hz) {
			out = append(out, hz)
		}
	}
	slices.Sort(out)
	return out
}

func (c *checker) dialFrequencies(ctx context.Context, d Dataset, mode, app string) ([]string, error) {
	src, err := c.fetch(ctx, d.Source)
	if err != nil {
		return nil, err
	}
	theirs := parseFrequencyList(src, mode)
	if len(theirs) == 0 {
		return nil, fmt.Errorf("found no %s frequencies; has the file's format changed?", mode)
	}
	ours, err := listHz(d.Covers[0])
	if err != nil {
		return nil, err
	}
	var out []string
	for _, hz := range theirs {
		if _, ok := ours[hz]; !ok && !d.ignored(mhz(hz)) {
			out = append(out, fmt.Sprintf("%s lists %s on %s MHz, which %s doesn't have.", app, mode, mhz(hz), d.Covers[0]))
		}
	}
	for _, hz := range sortedKeys(ours) {
		if !slices.Contains(theirs, hz) && !d.ignored(mhz(hz)) {
			out = append(out, fmt.Sprintf("%s has %s (%s MHz), which %s no longer lists.", d.Covers[0], ours[hz].Name, mhz(hz), app))
		}
	}
	return out, nil
}

func sortedKeys[V any](m map[int64]V) []int64 {
	keys := make([]int64, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// --- AMSAT FM satellites ---

type satellite struct {
	Name             string
	UplinkHz, DownHz int64
	ToneHz           string // "" when AMSAT lists none
}

var (
	tableRow  = regexp.MustCompile(`(?s)<tr.*?</tr>`)
	tableCell = regexp.MustCompile(`(?s)<t[dh].*?</t[dh]>`)
	tag       = regexp.MustCompile(`(?s)<.*?>`)
	mhzValue  = regexp.MustCompile(`(\d+\.\d+)\s*MHz`)
	ctcss     = regexp.MustCompile(`(\d+(?:\.\d+)?)\s*Hz CTCSS`)
)

func parseAMSAT(page []byte) []satellite {
	var out []satellite
	for _, row := range tableRow.FindAll(page, -1) {
		var cells []string
		for _, cell := range tableCell.FindAll(row, -1) {
			cells = append(cells, strings.TrimSpace(html.UnescapeString(tag.ReplaceAllString(string(cell), " "))))
		}
		if len(cells) < 3 {
			continue
		}
		up, down := mhzValue.FindStringSubmatch(cells[1]), mhzValue.FindStringSubmatch(cells[2])
		if up == nil || down == nil {
			continue // the header
		}
		s := satellite{Name: strings.TrimSpace(strings.SplitN(cells[0], "(", 2)[0])}
		s.UplinkHz, _ = domain.ParseFrequencyMHz(up[1])
		s.DownHz, _ = domain.ParseFrequencyMHz(down[1])
		if t := ctcss.FindStringSubmatch(cells[1]); t != nil {
			s.ToneHz = t[1]
		}
		out = append(out, s)
	}
	return out
}

func (c *checker) amsatFM(ctx context.Context, d Dataset) ([]string, error) {
	page, err := c.fetch(ctx, d.Source)
	if err != nil {
		return nil, err
	}
	sats := parseAMSAT(page)
	if len(sats) == 0 {
		return nil, fmt.Errorf("found no satellites; has the page's format changed?")
	}
	ours := map[string]domain.Channel{}
	chs, err := listHz(d.Covers[0])
	if err != nil {
		return nil, err
	}
	for _, ch := range chs {
		ours[ch.Name] = ch
	}
	ignored := func(name string) bool {
		return slices.ContainsFunc(d.Ignore, func(prefix string) bool { return strings.HasPrefix(name, prefix) })
	}
	var out []string
	listed := map[string]bool{}
	for _, s := range sats {
		listed[s.Name] = true
		ch, ok := ours[s.Name]
		switch {
		case ignored(s.Name):
		case !ok:
			out = append(out, fmt.Sprintf("AMSAT lists %s (up %s, down %s MHz), which %s doesn't have.", s.Name, mhz(s.UplinkHz), mhz(s.DownHz), d.Covers[0]))
		default:
			if ch.ReceiveHz != s.DownHz || ch.TransmitHz == nil || *ch.TransmitHz != s.UplinkHz {
				out = append(out, fmt.Sprintf("%s: AMSAT gives up %s, down %s MHz; %s has a different pair.", s.Name, mhz(s.UplinkHz), mhz(s.DownHz), d.Covers[0]))
			}
			if s.ToneHz != "" && !toneIs(ch.TransmitTone, s.ToneHz) {
				out = append(out, fmt.Sprintf("%s: AMSAT gives a %s Hz uplink tone; %s has %s.", s.Name, s.ToneHz, d.Covers[0], domain.FormatTone(ch.TransmitTone)))
			}
		}
	}
	for _, name := range sortedNames(ours) {
		if !listed[name] {
			out = append(out, fmt.Sprintf("%s has %s, which AMSAT no longer lists; it may be off the air.", d.Covers[0], name))
		}
	}
	return out, nil
}

func toneIs(t domain.Tone, hz string) bool {
	want, err := domain.ParseTone("ctcss:" + hz)
	return err == nil && domain.FormatTone(t) == domain.FormatTone(want)
}

func sortedNames[V any](m map[string]V) []string {
	names := make([]string, 0, len(m))
	for n := range m {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// --- NWS HF radiofax ---

// faxLine matches a schedule's frequency lines: an optional call sign,
// then the assigned frequency in kHz.
var faxLine = regexp.MustCompile(`(?m)^\s*([A-Z][A-Z0-9]{2,5})?\s+(\d+(?:\.\d+)?)\s+kHz`)

// parseRadiofax returns a schedule's call sign and assigned frequencies.
func parseRadiofax(src []byte) (string, []int64) {
	call := ""
	var hz []int64
	for _, m := range faxLine.FindAllSubmatch(src, -1) {
		if len(m[1]) > 0 {
			if call != "" && call != string(m[1]) {
				break // only the first station's block
			}
			call = string(m[1])
		}
		khz, err := strconv.ParseFloat(string(m[2]), 64)
		if err == nil {
			hz = append(hz, int64(khz*1000+0.5))
		}
	}
	return call, hz
}

// faxOffsetHz is how far the USB dial sits below the assigned frequency.
const faxOffsetHz = 1900

func (c *checker) radiofax(ctx context.Context, d Dataset) ([]string, error) {
	ours := map[string][]int64{} // call sign -> assigned Hz
	chs, err := listHz(d.Covers[0])
	if err != nil {
		return nil, err
	}
	for _, ch := range chs {
		call := strings.Fields(ch.Name)[0]
		ours[call] = append(ours[call], ch.ReceiveHz+faxOffsetHz)
	}
	var out []string
	seen := map[string]bool{}
	for _, url := range d.Sources {
		src, err := c.fetch(ctx, url)
		if errors.Is(err, errGone) {
			out = append(out, fmt.Sprintf("The schedule %s is gone; the station may have stopped broadcasting.", url))
			continue
		} else if err != nil {
			return nil, err
		}
		call, theirs := parseRadiofax(src)
		if call == "" {
			return nil, fmt.Errorf("%s: found no call sign; has the format changed?", url)
		}
		seen[call] = true
		mine := ours[call]
		for _, hz := range theirs {
			if !slices.Contains(mine, hz) {
				out = append(out, fmt.Sprintf("%s broadcasts on %s kHz (assigned), which %s doesn't have.", call, khz(hz), d.Covers[0]))
			}
		}
		for _, hz := range mine {
			if !slices.Contains(theirs, hz) {
				out = append(out, fmt.Sprintf("%s has %s on %s kHz (assigned), which the NWS schedule no longer lists.", d.Covers[0], call, khz(hz)))
			}
		}
	}
	for _, call := range sortedNames(ours) {
		if !seen[call] {
			out = append(out, fmt.Sprintf("%s has %s, but no checked schedule lists it.", d.Covers[0], call))
		}
	}
	sort.Strings(out)
	return out, nil
}

func khz(hz int64) string { return strconv.FormatFloat(float64(hz)/1e3, 'f', -1, 64) }

// --- eCFR ---

type ecfrVersions struct {
	ContentVersions []struct {
		Identifier    string `json:"identifier"`
		Name          string `json:"name"`
		AmendmentDate string `json:"amendment_date"`
	} `json:"content_versions"`
}

func (c *checker) ecfr(ctx context.Context, d Dataset) ([]string, error) {
	url := fmt.Sprintf("https://www.ecfr.gov/api/versioner/v1/versions/title-%d.json?part=%d", d.Title, d.Part)
	src, err := c.fetch(ctx, url)
	if err != nil {
		return nil, err
	}
	var v ecfrVersions
	if err := json.Unmarshal(src, &v); err != nil {
		return nil, fmt.Errorf("read eCFR versions: %w", err)
	}
	latest, names := map[string]string{}, map[string]string{}
	for _, cv := range v.ContentVersions {
		if cv.AmendmentDate > latest[cv.Identifier] {
			latest[cv.Identifier] = cv.AmendmentDate
			names[cv.Identifier] = strings.Join(strings.Fields(cv.Name), " ")
		}
	}
	var out []string
	for _, sec := range sortedNames(d.Recorded) {
		got, was := latest[sec], d.Recorded[sec]
		switch {
		case got == "":
			out = append(out, fmt.Sprintf("§%s no longer appears in the eCFR.", sec))
		case was == "":
			out = append(out, fmt.Sprintf("%s (amended %s) hasn't been reviewed against rflib's data yet.", names[sec], got))
		case got != was:
			out = append(out, fmt.Sprintf("%s was amended %s; rflib's data was reviewed against the %s version.", names[sec], got, was))
		}
	}
	return out, nil
}

// --- CHIRP ---

// chirpDump is `tools/chirp_model.py --json`.
type chirpDump struct {
	Version string                `json:"chirp_version"`
	Radios  map[string]chirpRadio `json:"radios"`
	Failed  map[string]string     `json:"failed"`
}

type chirpRadio struct {
	Driver   string `json:"driver"`
	Memories struct {
		First int `json:"first"`
		Last  int `json:"last"`
	} `json:"memories"`
	Names struct {
		MaxLength int `json:"max_length"`
	} `json:"names"`
	Power []struct {
		Label string  `json:"label"`
		Watts float64 `json:"watts"`
	} `json:"power"`
	BandsMHz [][2]float64 `json:"bands_mhz"`
	Modes    []string     `json:"modes"`
}

func loadChirp(path string) (*chirpDump, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var d chirpDump
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if len(d.Radios) == 0 {
		return nil, fmt.Errorf("%s lists no radios", path)
	}
	return &d, nil
}

// chirpModels compares curated models with what CHIRP's drivers declare.
// Ignore entries read "<model>: <field>".
func (c *checker) chirpModels(d Dataset) ([]string, error) {
	if c.chirp == nil {
		return nil, errSkipped
	}
	var out []string
	for _, name := range d.Covers {
		m, err := model.Find(name)
		if err != nil {
			return nil, err
		}
		r, ok := c.chirp.Radios[m.ChirpModel]
		if !ok {
			out = append(out, fmt.Sprintf("%s: CHIRP no longer has a radio named %q.", name, m.ChirpModel))
			continue
		}
		differs := func(field, ours, theirs string) {
			if ours != theirs && !d.ignored(name+": "+field) {
				out = append(out, fmt.Sprintf("%s: %s is %s; CHIRP's driver (%s) says %s.", name, field, ours, r.Driver, theirs))
			}
		}
		differs("memories", fmt.Sprintf("%d-%d", m.Memories.First, m.Memories.Last), fmt.Sprintf("%d-%d", r.Memories.First, r.Memories.Last))
		differs("name length", strconv.Itoa(m.Names.MaxLength), strconv.Itoa(r.Names.MaxLength))
		differs("modes", strings.Join(m.Modes, ","), strings.Join(r.Modes, ","))
		differs("bands", bands(m.BandsMHz), bands(r.BandsMHz))
		var ours, theirs []string
		for _, l := range m.Power {
			if l.Watts != nil {
				ours = append(ours, strconv.FormatFloat(*l.Watts, 'f', -1, 64))
			}
		}
		for _, l := range r.Power {
			theirs = append(theirs, strconv.FormatFloat(l.Watts, 'f', -1, 64))
		}
		differs("power watts", orNone(ours), orNone(theirs))
	}
	return out, nil
}

// bands formats bands in order, to the kHz, since drivers list them in
// any order and some end a band at 135.999999.
func bands(bs [][2]float64) string {
	sorted := slices.Clone(bs)
	slices.SortFunc(sorted, func(a, b [2]float64) int { return cmp.Compare(a[0], b[0]) })
	parts := make([]string, len(sorted))
	for i, b := range sorted {
		parts[i] = kHzMHz(b[0]) + "-" + kHzMHz(b[1])
	}
	return orNone(parts)
}

func kHzMHz(f float64) string { return strconv.FormatFloat(math.Round(f*1000)/1000, 'f', -1, 64) }

func orNone(parts []string) string {
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, ",")
}

// chirpRadios compares CHIRP's radios with the baseline last reviewed.
func (c *checker) chirpRadios(d Dataset) ([]string, error) {
	if c.chirp == nil {
		return nil, errSkipped
	}
	data, err := os.ReadFile(filepath.Join(c.dir, d.Baseline))
	if err != nil {
		return nil, err
	}
	baseline := map[string]bool{}
	for line := range strings.SplitSeq(string(data), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			baseline[line] = true
		}
	}
	var out []string
	for _, name := range sortedNames(c.chirp.Radios) {
		if !baseline[name] {
			out = append(out, fmt.Sprintf("New in CHIRP: %s (%s). Consider a curated model if users ask for it.", name, c.chirp.Radios[name].Driver))
		}
	}
	for _, name := range sortedNames(baseline) {
		if _, ok := c.chirp.Radios[name]; !ok {
			out = append(out, fmt.Sprintf("No longer in CHIRP: %s.", name))
		}
	}
	for _, name := range sortedNames(c.chirp.Failed) {
		out = append(out, fmt.Sprintf("CHIRP's driver for %s couldn't describe itself: %s.", name, c.chirp.Failed[name]))
	}
	if len(out) > 0 {
		out = append(out, fmt.Sprintf("Once reviewed, update %s from `uv run tools/chirp_model.py --list`.", d.Baseline))
	}
	return out, nil
}
