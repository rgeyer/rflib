// Package naming fits channel names to radio displays. It picks the longest
// name a radio can show and, when nothing fits, shortens step by step,
// stopping as soon as the name fits:
//
//  1. built-in dictionary abbreviations, largest saving first
//  2. drop filler words
//  3. remove spaces, capitalizing each word so boundaries stay readable
//  4. trim common words toward their initial (when naming a whole export,
//     words shared by many channels, such as an agency prefix, tell them
//     apart least, so they give way first)
//  5. remove inner vowels, longest words first
//  6. trim words toward their initial, leftmost first, only as far as needed
//  7. cut what's left from the right
//
// Brackets, slashes, and similar punctuation count as word breaks while
// shortening, as do run-together words ("SantaBarbaraPD1" is "Santa Barbara
// PD1"). Identifier tokens (any word containing a digit, such as "15R",
// "1D", or a callsign) are never abbreviated, de-voweled, or cut while
// anything else can give way, so similar channels stay distinct. In a
// mixed-case name, mostly-capital words ("ARES", "SBCoSO") are treated as
// acronyms and keep their letters until the cut. Results are deterministic:
// the same input always gives the same name.
package naming

import (
	_ "embed"
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Method says how a name was produced.
type Method string

const (
	Fits      Method = "fits"      // the full name fits
	Variant   Method = "variant"   // one of the channel's short names fits
	Shortened Method = "shortened" // abbreviations, spaces, or vowels removed
	Cut       Method = "cut"       // characters cut after shortening wasn't enough
)

// Rules describe what a radio can display.
type Rules struct {
	// MaxLen is the display width in characters; 0 means unlimited.
	MaxLen int
	// Upper converts names to upper case.
	Upper bool
	// Charset limits the characters used; nil means printable ASCII.
	Charset *Charset

	// common holds lower-cased words shared by many channels in an export;
	// Apply sets it.
	common map[string]bool
}

func (r Rules) allows(c rune) bool {
	if r.Charset == nil {
		return c >= 0x20 && c <= 0x7e
	}
	return r.Charset.Allows(c)
}

// Result is the fitted name for one channel.
type Result struct {
	Name   string
	Method Method
	// Deduped is set when a suffix was added to tell the name apart from
	// another channel's.
	Deduped bool
}

// Fit chooses a channel's name for a radio: the full name if it fits, else
// the longest short name that fits, else the shortest of them shortened.
// Shortening starts from the shortest because a user's own short names
// already carry abbreviations the dictionary can't know.
func Fit(name string, shortNames []string, r Rules) Result {
	full := clean(name, r)
	if r.MaxLen <= 0 || runeLen(display(full, r)) <= r.MaxLen {
		return Result{Name: display(full, r), Method: Fits}
	}
	start := full
	best := ""
	for _, s := range shortNames {
		s = clean(s, r)
		n := runeLen(display(s, r))
		if n == 0 {
			continue
		}
		if n <= r.MaxLen && n > runeLen(best) {
			best = display(s, r)
		}
		if n < runeLen(display(start, r)) {
			start = s
		}
	}
	if best != "" {
		return Result{Name: best, Method: Variant}
	}
	name, method := shorten(start, r)
	return Result{Name: name, Method: method}
}

type token struct {
	text string
	// protected marks identifiers, which only the final cut may touch.
	protected bool
	// acronym marks mostly-capital words in a mixed-case name, which keep
	// their vowels and aren't reduced to initials.
	acronym bool
	// common marks words shared by many channels in the export.
	common bool
}

// wordBreaks are treated as spaces while shortening, so "Control/Lake" and
// "(Lompoc ARES)" split into words the dictionary can match.
var wordBreaks = strings.NewReplacer("/", " ", "(", " ", ")", " ", "[", " ", "]", " ", ",", " ", ";", " ", ":", " ")

func tokenize(s string) []token {
	mixedCase := strings.ToUpper(s) != s
	var toks []token
	for field := range strings.FieldsSeq(wordBreaks.Replace(s)) {
		for _, f := range splitCamel(field) {
			toks = append(toks, token{text: f, protected: strings.ContainsFunc(f, unicode.IsDigit), acronym: mixedCase && mostlyCapitals(f)})
		}
	}
	return toks
}

// splitCamel splits run-together words at a capital that follows two or more
// lower-case letters. One lower-case letter isn't enough, so initialisms such
// as "SBCoSO" and names such as "McDonald" stay whole.
func splitCamel(s string) []string {
	var parts []string
	rs := []rune(s)
	start, lowerRun := 0, 0
	for i, c := range rs {
		if unicode.IsUpper(c) && lowerRun >= 2 {
			parts = append(parts, string(rs[start:i]))
			start = i
		}
		if unicode.IsLower(c) {
			lowerRun++
		} else {
			lowerRun = 0
		}
	}
	return append(parts, string(rs[start:]))
}

func mostlyCapitals(s string) bool {
	upper, letters := 0, 0
	for _, c := range s {
		if unicode.IsLetter(c) {
			letters++
			if unicode.IsUpper(c) {
				upper++
			}
		}
	}
	return letters >= 2 && upper*2 >= letters
}

func join(toks []token, sep string) string {
	parts := make([]string, len(toks))
	for i, t := range toks {
		parts[i] = t.text
	}
	return strings.Join(parts, sep)
}

var fillerWords = map[string]bool{"the": true, "of": true, "and": true, "a": true, "an": true, "at": true, "for": true, "to": true, "on": true}

func shorten(s string, r Rules) (string, Method) {
	toks := tokenize(s)
	for i := range toks {
		toks[i].common = !toks[i].protected && r.common[strings.ToLower(toks[i].text)]
	}
	sep := " "
	if !r.allows(' ') {
		// Words run together from the start, so capitalize them now.
		sep = ""
		for i := range toks {
			toks[i].text = capitalize(toks[i].text)
		}
	}
	fits := func() bool { return runeLen(join(toks, sep)) <= r.MaxLen }

	// 1. Dictionary abbreviations, largest saving first.
	for !fits() {
		i, e, ok := bestAbbreviation(toks, sep)
		if !ok {
			break
		}
		repl := token{text: clean(e.short, r)}
		toks = append(toks[:i], append([]token{repl}, toks[i+len(e.long):]...)...)
	}
	if fits() {
		return join(toks, sep), Shortened
	}

	// 2. Filler words, left to right. Only lower-case ones go ("A" in
	// "Diablo A" is a site, not an article), and never the last word.
	for i := 0; i < len(toks) && !fits(); {
		if !toks[i].protected && fillerWords[toks[i].text] && i < len(toks)-1 && unprotected(toks) > 1 {
			toks = append(toks[:i], toks[i+1:]...)
			continue
		}
		i++
	}
	if fits() {
		return join(toks, sep), Shortened
	}

	// 3. Remove spaces, capitalizing words so boundaries stay readable.
	for i := range toks {
		toks[i].text = capitalize(toks[i].text)
	}
	sep = ""
	if fits() {
		return join(toks, sep), Shortened
	}

	// 4. Common words, leftmost first, toward their initial.
	if trimToward(toks, sep, r.MaxLen, func(t token) bool { return t.common }) {
		return join(toks, sep), Shortened
	}

	// 5. Inner vowels, longest unprotected words first.
	order := make([]int, 0, len(toks))
	for i, t := range toks {
		if !t.protected && !t.acronym && runeLen(t.text) > 3 {
			order = append(order, i)
		}
	}
	sort.SliceStable(order, func(a, b int) bool { return runeLen(toks[order[a]].text) > runeLen(toks[order[b]].text) })
	for _, i := range order {
		if v := dropVowels(toks[i].text); runeLen(v) >= 2 {
			toks[i].text = v
		}
		if fits() {
			return join(toks, sep), Shortened
		}
	}

	// 6. Other words toward their initial, leftmost first, so later words
	// (usually the channel's function) survive.
	if trimToward(toks, sep, r.MaxLen, func(t token) bool { return !t.protected && !t.acronym }) {
		return join(toks, sep), Shortened
	}

	// 7. Cut from the right, sparing identifiers while anything else is left.
	need := runeLen(join(toks, sep)) - r.MaxLen
	for i := len(toks) - 1; i >= 0 && need > 0; i-- {
		if toks[i].protected {
			continue
		}
		rs := []rune(toks[i].text)
		k := min(need, len(rs))
		toks[i].text = string(rs[:len(rs)-k])
		need -= k
	}
	out := join(toks, sep)
	if rs := []rune(out); len(rs) > r.MaxLen {
		out = string(rs[:r.MaxLen]) // identifiers alone are too long
	}
	return out, Cut
}

// trimToward shortens the chosen words, leftmost first and only as far as
// needed, but never below their first letter. It reports whether the name
// then fits.
func trimToward(toks []token, sep string, max int, choose func(token) bool) bool {
	for i := range toks {
		if !choose(toks[i]) || runeLen(toks[i].text) <= 1 {
			continue
		}
		rs := []rune(toks[i].text)
		k := min(runeLen(join(toks, sep))-max, len(rs)-1)
		toks[i].text = string(rs[:len(rs)-k])
		if runeLen(join(toks, sep)) <= max {
			return true
		}
	}
	return false
}

func unprotected(toks []token) int {
	n := 0
	for _, t := range toks {
		if !t.protected {
			n++
		}
	}
	return n
}

// bestAbbreviation finds the dictionary match that saves the most
// characters; ties go to the earliest position, then the dictionary order.
func bestAbbreviation(toks []token, sep string) (int, abbreviation, bool) {
	bestPos, bestSaving := -1, 0
	var best abbreviation
	for i := range toks {
		for _, e := range dictionary {
			if i+len(e.long) > len(toks) || !matches(toks[i:i+len(e.long)], e.long) {
				continue
			}
			span := runeLen(join(toks[i:i+len(e.long)], sep))
			if saving := span - runeLen(e.short); saving > bestSaving {
				bestPos, bestSaving, best = i, saving, e
			}
		}
	}
	return bestPos, best, bestPos >= 0
}

func matches(toks []token, words []string) bool {
	for i, w := range words {
		if toks[i].protected || !strings.EqualFold(toks[i].text, w) {
			return false
		}
	}
	return true
}

func capitalize(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	if n == 0 {
		return s
	}
	return string(unicode.ToUpper(r)) + s[n:]
}

// dropVowels removes vowels after the first letter. In mixed-case words only
// lower-case vowels go, since capitals there are usually initials ("SBCoSO"
// keeps its O); all-capital words lose capital vowels too.
func dropVowels(s string) string {
	allCaps := strings.ToUpper(s) == s
	var b strings.Builder
	for i, c := range s {
		lower := unicode.ToLower(c)
		vowel := lower == 'a' || lower == 'e' || lower == 'i' || lower == 'o' || lower == 'u'
		if i > 0 && vowel && (unicode.IsLower(c) || allCaps) {
			continue
		}
		b.WriteRune(c)
	}
	return b.String()
}

// clean transliterates accented letters, applies case and charset, and
// collapses whitespace. Spaces are kept even when the charset excludes them,
// so shortening still sees words; display removes them at the end.
func clean(s string, r Rules) string {
	var b strings.Builder
	for _, c := range s {
		for _, t := range transliterate(c) {
			if unicode.IsSpace(t) {
				t = ' '
			}
			if r.Upper {
				t = unicode.ToUpper(t)
			}
			if t == ' ' || r.allows(t) {
				b.WriteRune(t)
			}
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// display is how a cleaned name appears on the radio: without spaces when
// the charset excludes them.
func display(s string, r Rules) string {
	if r.allows(' ') {
		return s
	}
	return strings.ReplaceAll(s, " ", "")
}

var translit = map[rune]string{
	'á': "a", 'à': "a", 'â': "a", 'ä': "a", 'ã': "a", 'å': "a", 'Á': "A", 'À': "A", 'Â': "A", 'Ä': "A", 'Ã': "A", 'Å': "A",
	'é': "e", 'è': "e", 'ê': "e", 'ë': "e", 'É': "E", 'È': "E", 'Ê': "E", 'Ë': "E",
	'í': "i", 'ì': "i", 'î': "i", 'ï': "i", 'Í': "I", 'Ì': "I", 'Î': "I", 'Ï': "I",
	'ó': "o", 'ò': "o", 'ô': "o", 'ö': "o", 'õ': "o", 'ø': "o", 'Ó': "O", 'Ò': "O", 'Ô': "O", 'Ö': "O", 'Õ': "O", 'Ø': "O",
	'ú': "u", 'ù': "u", 'û': "u", 'ü': "u", 'Ú': "U", 'Ù': "U", 'Û': "U", 'Ü': "U",
	'ñ': "n", 'Ñ': "N", 'ç': "c", 'Ç': "C", 'ß': "ss", '’': "'", '‘': "'", '–': "-", '—': "-",
}

func transliterate(c rune) string {
	if t, ok := translit[c]; ok {
		return t
	}
	return string(c)
}

func runeLen(s string) int { return utf8.RuneCountInString(s) }

// Charset is a set of allowed characters.
type Charset struct {
	spec   string
	ranges [][2]rune
}

func (c *Charset) Allows(r rune) bool {
	for _, rg := range c.ranges {
		if r >= rg[0] && r <= rg[1] {
			return true
		}
	}
	return false
}

func (c *Charset) String() string { return c.spec }

// ParseCharset reads "ascii" (printable ASCII), "alnum" (letters, digits,
// and space), or a literal set where "A-Z" is a range and a "-" at either
// end is literal, such as "A-Z0-9 -/".
func ParseCharset(spec string) (*Charset, error) {
	switch spec {
	case "", "ascii":
		return &Charset{spec: "ascii", ranges: [][2]rune{{0x20, 0x7e}}}, nil
	case "alnum":
		return &Charset{spec: spec, ranges: [][2]rune{{'A', 'Z'}, {'a', 'z'}, {'0', '9'}, {' ', ' '}}}, nil
	}
	rs := []rune(spec)
	c := &Charset{spec: spec}
	for i := 0; i < len(rs); i++ {
		if i+2 < len(rs) && rs[i+1] == '-' {
			if rs[i] > rs[i+2] {
				return nil, fmt.Errorf("charset range %c-%c is backwards", rs[i], rs[i+2])
			}
			c.ranges = append(c.ranges, [2]rune{rs[i], rs[i+2]})
			i += 2
			continue
		}
		c.ranges = append(c.ranges, [2]rune{rs[i], rs[i]})
	}
	return c, nil
}

//go:embed abbreviations.txt
var abbreviationsText string

type abbreviation struct {
	long  []string
	short string
}

var dictionary = parseDictionary(abbreviationsText)

func parseDictionary(text string) []abbreviation {
	var out []abbreviation
	for line := range strings.SplitSeq(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		long, short, ok := strings.Cut(line, "=")
		if !ok {
			panic("naming: bad abbreviation line: " + line)
		}
		out = append(out, abbreviation{long: strings.Fields(long), short: strings.TrimSpace(short)})
	}
	return out
}
