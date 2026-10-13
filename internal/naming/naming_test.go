package naming

import (
	"strings"
	"testing"

	"github.com/ryan/rflib/internal/domain"
)

func TestFit(t *testing.T) {
	for _, tc := range []struct {
		name   string
		max    int
		want   string
		method Method
	}{
		{"Metro Repeater", 0, "Metro Repeater", Fits},
		{"Metro Repeater", 14, "Metro Repeater", Fits},
		{"Metro Repeater", 12, "Metro Rpt", Shortened},     // dictionary only as far as needed
		{"Metro Repeater", 8, "MetroRpt", Shortened},       // then spaces
		{"Metro Repeater", 6, "MtrRpt", Shortened},         // then vowels
		{"Search and Rescue Ch1", 8, "SAR Ch1", Shortened}, // phrase before filler drop
		{"Common Traffic Advisory Frequency", 6, "CTAF", Shortened},
		{"Santa Barbara Sheriff Dispatch", 8, "SBShrDsp", Shortened}, // words trimmed toward initials
		{"Santa Barbara Sheriff Dispatch", 6, "SBSDsp", Shortened},
		{"RitzCarlton Ops", 8, "RCrltOps", Shortened},                          // trimmed only as far as needed
		{"Animal Control/Cachuma Lake Park", 16, "ACCachumaLkPark", Shortened}, // slash is a word break
		{"Harris Grade (Lompoc ARES)", 16, "HrrsGrdLmpcARES", Shortened},       // brackets dropped, acronym kept
		{"Lompoc ARES Repeater", 8, "LARESRpt", Shortened},
		{"NOAA WX1", 6, "NOAWX1", Shortened}, // vowels never reduce a word to one letter
		{"W6AF-ORC", 6, "W6AF-O", Cut},       // identifiers alone are cut last
		{"  Lots   of\tspace ", 0, "Lots of space", Fits},
		{"SantaBarbaraPD1", 8, "SBrbrPD1", Shortened}, // run-together words split
		{"SantaBarbaraPrkg", 6, "SBPrkg", Shortened},
		{"Diablo A", 6, "DblA", Shortened},                                    // "A" is a site letter, not filler
		{"SBC A/G", 5, "SBCAG", Shortened},                                    // slash is a word break
		{"Office of the Sheriff Dispatch", 18, "Office Sher Disp", Shortened}, // lower-case filler goes
	} {
		got := Fit(tc.name, nil, Rules{MaxLen: tc.max})
		if got.Name != tc.want || got.Method != tc.method {
			t.Errorf("Fit(%q, %d) = %q (%s), want %q (%s)", tc.name, tc.max, got.Name, got.Method, tc.want, tc.method)
		}
		if tc.max > 0 && len([]rune(got.Name)) > tc.max {
			t.Errorf("Fit(%q, %d) = %q is too long", tc.name, tc.max, got.Name)
		}
	}
}

// Identifiers keep otherwise identical names apart at any width.
func TestIdentifiersStayDistinct(t *testing.T) {
	for _, max := range []int{16, 12, 8, 7, 6, 5, 4} {
		a := Fit("SBCoSO Ops 1D", nil, Rules{MaxLen: max}).Name
		b := Fit("SBCoSO Ops 2D", nil, Rules{MaxLen: max}).Name
		if a == b || !strings.HasSuffix(a, "1D") || !strings.HasSuffix(b, "2D") {
			t.Errorf("at %d: %q and %q", max, a, b)
		}
	}
}

func TestShortNames(t *testing.T) {
	short := []string{"SB Sheriff Disp", "SBSO Disp"}
	if got := Fit("Santa Barbara Sheriff Dispatch", short, Rules{MaxLen: 16}); got.Name != "SB Sheriff Disp" || got.Method != Variant {
		t.Errorf("16: %+v", got)
	}
	if got := Fit("Santa Barbara Sheriff Dispatch", short, Rules{MaxLen: 10}); got.Name != "SBSO Disp" || got.Method != Variant {
		t.Errorf("10: %+v", got)
	}
	// Nothing fits: shorten the user's shortest name, which keeps "SBSO".
	if got := Fit("Santa Barbara Sheriff Dispatch", short, Rules{MaxLen: 6}); got.Name != "SBSODs" || got.Method != Shortened {
		t.Errorf("6: %+v", got)
	}
}

func TestCaseAndCharset(t *testing.T) {
	alnum, err := ParseCharset("alnum")
	if err != nil {
		t.Fatal(err)
	}
	if got := Fit("Café Méditerranée", nil, Rules{}); got.Name != "Cafe Mediterranee" {
		t.Errorf("transliteration: %q", got.Name)
	}
	if got := Fit("W6AF-ORC (Main)", nil, Rules{Charset: alnum, Upper: true}); got.Name != "W6AFORC MAIN" {
		t.Errorf("alnum upper: %q", got.Name)
	}
	upper, err := ParseCharset("A-Z0-9-")
	if err != nil {
		t.Fatal(err)
	}
	if got := Fit("Metro Repeater 2", nil, Rules{MaxLen: 8, Charset: upper, Upper: true}); got.Name != "MTRRPT2" {
		t.Errorf("no spaces allowed: %q", got.Name)
	}
	if _, err := ParseCharset("Z-A"); err == nil {
		t.Error("accepted a backwards range")
	}
	if c, _ := ParseCharset("-A-C"); !c.Allows('-') || !c.Allows('B') || c.Allows('D') {
		t.Error("literal leading dash or range parsed wrong")
	}
}

func TestDeterministic(t *testing.T) {
	for i := 0; i < 50; i++ {
		if got := Fit("Santa Barbara County Fire Department Station 12 Dispatch", nil, Rules{MaxLen: 10}).Name; got != "SBCFS12Dsp" {
			t.Fatalf("run %d: %q", i, got)
		}
	}
}

func channels(names ...string) []domain.Channel {
	cs := make([]domain.Channel, len(names))
	for i, n := range names {
		cs[i] = domain.Channel{Name: n, ReceiveHz: 146_520_000}
	}
	return cs
}

// The four "RitzCarlton Ops" channels from a RadioReference export.
func ritz() []domain.Channel {
	cs := channels("RitzCarlton Ops", "RitzCarlton Ops", "RitzCarlton Ops", "RitzCarlton Ops")
	for i, hz := range []int64{464_650_000, 464_750_000, 464_850_000, 464_700_000} {
		cs[i].ReceiveHz = hz
	}
	return cs
}

func names(rs []Result) string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.Name
	}
	return strings.Join(out, "|")
}

func TestDedupe(t *testing.T) {
	for _, tc := range []struct {
		max  int
		d    Dedupe
		want string
	}{
		{0, DedupeNone, "RitzCarlton Ops|RitzCarlton Ops|RitzCarlton Ops|RitzCarlton Ops"},
		{0, DedupeIndex, "RitzCarlton Ops 1|RitzCarlton Ops 2|RitzCarlton Ops 3|RitzCarlton Ops 4"},
		{0, DedupeFreq, "RitzCarlton Ops 65|RitzCarlton Ops 75|RitzCarlton Ops 85|RitzCarlton Ops 47"},
		{8, DedupeIndex, "RCarOps1|RCarOps2|RCarOps3|RCarOps4"}, // shared words give way first
		{8, DedupeFreq, "RCaOps65|RCaOps75|RCaOps85|RCaOps47"},
		{16, DedupeIndex, "RitzCarlton Ops1|RitzCarlton Ops2|RitzCarlton Ops3|RitzCarlton Ops4"}, // no room for a space
	} {
		renamed, rs := Apply(ritz(), Rules{MaxLen: tc.max}, tc.d)
		if got := names(rs); got != tc.want {
			t.Errorf("max %d, %s:\n got %s\nwant %s", tc.max, tc.d, got, tc.want)
		}
		if renamed[0].Name != rs[0].Name || (tc.d != DedupeNone) != rs[0].Deduped {
			t.Errorf("max %d, %s: renamed channels or Deduped flag wrong", tc.max, tc.d)
		}
	}

	// Channels sharing a frequency can't be told apart by it: number them.
	_, rs := Apply(channels("Signature", "Signature"), Rules{}, DedupeFreq)
	if got := names(rs); got != "Signature 1|Signature 2" {
		t.Errorf("same frequency: %s", got)
	}
	// A suffix that collides with another channel's name is renumbered.
	_, rs = Apply(channels("Ops", "Ops", "Ops 1"), Rules{}, DedupeIndex)
	if got := names(rs); len(Duplicates(rs)) != 0 {
		t.Errorf("still duplicated: %s", got)
	}
	// Collisions created by shortening count, case-insensitively.
	_, rs = Apply(channels("Metro Rpt", "METRO RPT", "Metro Repeater"), Rules{MaxLen: 9}, DedupeNone)
	if len(Duplicates(rs)) != 1 {
		t.Errorf("expected one duplicate group: %s", names(rs))
	}
}

func TestDictionary(t *testing.T) {
	seen := map[string]bool{}
	for _, e := range dictionary {
		key := strings.ToLower(strings.Join(e.long, " "))
		if seen[key] {
			t.Errorf("duplicate entry %q", key)
		}
		seen[key] = true
		if len(e.short) == 0 || len(e.short) >= len(key) {
			t.Errorf("%q = %q doesn't shorten", key, e.short)
		}
	}
	if len(dictionary) < 50 {
		t.Errorf("dictionary has only %d entries", len(dictionary))
	}
}

// Words shared by many channels in an export give way before the words
// that tell them apart.
func TestCommonWordsGiveWayFirst(t *testing.T) {
	cs := channels("SBCoSO S Tac", "SBCoSO N Tac", "SBCoSO Dispatch", "SBCoSO Ops 1D", "SBCoSO Ops 2D", "Other Name")
	_, rs := Apply(cs, Rules{MaxLen: 6}, DedupeNone)
	if got := names(rs); got != "SBSTac|SBNTac|SBDisp|SOps1D|SOps2D|OthrNm" {
		t.Errorf("got %s", got)
	}
	if len(Duplicates(rs)) != 0 {
		t.Errorf("duplicates: %s", names(rs))
	}
	// Without export context the same name keeps its prefix.
	if got := Fit("SBCoSO S Tac", nil, Rules{MaxLen: 6}).Name; got != "SBCoSO" {
		t.Errorf("alone: %q", got)
	}
}

func TestSplitCamel(t *testing.T) {
	for in, want := range map[string]string{
		"SantaBarbaraPD1": "Santa|Barbara|PD1", "FlyingFlagsRV": "Flying|Flags|RV", "XferStn": "Xfer|Stn",
		"SBCoSO": "SBCoSO", "McDonald": "McDonald", "metro": "metro", "KCOY": "KCOY",
	} {
		if got := strings.Join(splitCamel(in), "|"); got != want {
			t.Errorf("splitCamel(%q) = %q, want %q", in, got, want)
		}
	}
}
