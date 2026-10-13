package naming

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ryan/rflib/internal/domain"
)

// Dedupe is how names that collide within one export are told apart.
type Dedupe string

const (
	DedupeNone  Dedupe = "none"  // leave duplicates as they are
	DedupeIndex Dedupe = "index" // append 1, 2, 3...
	DedupeFreq  Dedupe = "freq"  // append the shortest frequency digits that differ
)

func ParseDedupe(v string) (Dedupe, error) {
	switch d := Dedupe(v); d {
	case DedupeNone, DedupeIndex, DedupeFreq:
		return d, nil
	}
	return "", fmt.Errorf("unknown dedupe %q; use none, index, or freq", v)
}

// Apply fits every channel's name to the rules, then resolves names that
// collide (case-insensitively) with the dedupe strategy. It returns renamed
// copies of the channels and, for each, how its name was produced.
func Apply(cs []domain.Channel, r Rules, d Dedupe) ([]domain.Channel, []Result) {
	r.common = commonWords(cs, r)
	results := make([]Result, len(cs))
	for i, c := range cs {
		results[i] = fitChannel(c, r)
	}
	if d != DedupeNone && d != "" {
		strategy := d
		// A suffix can collide with another channel's name, so repeat;
		// later rounds number what's left.
		for round := 0; round < 3; round++ {
			groups := collisions(results)
			if len(groups) == 0 {
				break
			}
			for _, idx := range groups {
				for j, sfx := range suffixes(cs, idx, strategy) {
					i := idx[j]
					results[i] = Result{Name: withSuffix(cs[i], sfx, r), Method: results[i].Method, Deduped: true}
				}
			}
			strategy = DedupeIndex
		}
	}
	out := make([]domain.Channel, len(cs))
	for i, c := range cs {
		c.Name = results[i].Name
		out[i] = c
	}
	return out, results
}

// commonThreshold is how many channels must share a word for it to count as
// common in an export.
const commonThreshold = 3

// commonWords finds words that appear in the names of at least
// commonThreshold channels.
func commonWords(cs []domain.Channel, r Rules) map[string]bool {
	counts := map[string]int{}
	for _, c := range cs {
		seen := map[string]bool{}
		for _, t := range tokenize(clean(c.Name, r)) {
			if k := strings.ToLower(t.text); !t.protected && !seen[k] {
				seen[k] = true
				counts[k]++
			}
		}
	}
	common := map[string]bool{}
	for k, n := range counts {
		if n >= commonThreshold {
			common[k] = true
		}
	}
	return common
}

// Duplicates returns the indexes of channels whose fitted names collide, in
// groups.
func Duplicates(results []Result) [][]int { return collisions(results) }

// fitChannel fits a channel's name, falling back to its frequency when
// nothing displayable is left.
func fitChannel(c domain.Channel, r Rules) Result {
	res := Fit(c.Name, c.ShortNames, r)
	if res.Name == "" {
		res = Fit(strings.TrimRight(strings.TrimRight(domain.FormatFrequencyMHz(c.ReceiveHz), "0"), "."), nil, r)
	}
	return res
}

func collisions(results []Result) [][]int {
	byName := map[string][]int{}
	var order []string
	for i, res := range results {
		k := strings.ToLower(res.Name)
		if _, seen := byName[k]; !seen {
			order = append(order, k)
		}
		byName[k] = append(byName[k], i)
	}
	var groups [][]int
	for _, k := range order {
		if len(byName[k]) > 1 {
			groups = append(groups, byName[k])
		}
	}
	return groups
}

// suffixes returns one suffix per channel in the group. Frequency suffixes
// are the shortest trailing digits of the receive frequency that differ
// across the group; channels sharing a frequency fall back to numbering.
func suffixes(cs []domain.Channel, idx []int, d Dedupe) []string {
	out := make([]string, len(idx))
	if d == DedupeFreq {
		digits := make([]string, len(idx))
		distinct := map[string]bool{}
		maxLen := 0
		for j, i := range idx {
			digits[j] = strings.TrimRight(strconv.FormatInt(cs[i].ReceiveHz, 10), "0")
			distinct[digits[j]] = true
			maxLen = max(maxLen, len(digits[j]))
		}
		if len(distinct) == len(idx) {
			for k := 1; k <= maxLen; k++ {
				seen := map[string]bool{}
				for j, s := range digits {
					out[j] = s[max(0, len(s)-k):]
					seen[out[j]] = true
				}
				if len(seen) == len(idx) {
					return out
				}
			}
		}
	}
	for j := range idx {
		out[j] = strconv.Itoa(j + 1)
	}
	return out
}

// withSuffix fits the channel's name into the room left by the suffix,
// separating them with a space when the name then needs no shortening.
func withSuffix(c domain.Channel, sfx string, r Rules) string {
	if r.MaxLen <= 0 {
		return fitChannel(c, r).Name + " " + sfx
	}
	if r.allows(' ') {
		room := r
		room.MaxLen = r.MaxLen - runeLen(sfx) - 1
		if room.MaxLen > 0 {
			if res := fitChannel(c, room); res.Method == Fits || res.Method == Variant {
				return res.Name + " " + sfx
			}
		}
	}
	room := r
	room.MaxLen = r.MaxLen - runeLen(sfx)
	if room.MaxLen <= 0 {
		return string([]rune(sfx)[:r.MaxLen])
	}
	return fitChannel(c, room).Name + sfx
}
