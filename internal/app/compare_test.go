package app

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/ryan/rflib/internal/domain"
	"github.com/ryan/rflib/internal/export"
	"github.com/ryan/rflib/internal/program"
)

// radioFile writes a CHIRP file of what a radio holds, by memory number.
func radioFile(t *testing.T, mems map[int]domain.Channel) string {
	t.Helper()
	var locs []int
	var cs []domain.Channel
	for loc := 1; loc <= 20; loc++ {
		if c, ok := mems[loc]; ok {
			locs, cs = append(locs, loc), append(cs, c)
		}
	}
	var buf bytes.Buffer
	if err := (export.CHIRP{Locations: locs}).Export(&buf, cs); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func analog(name string, rx, tx float64, tone float64) domain.Channel {
	c := domain.Channel{Name: name, ReceiveHz: int64(rx * 1e6), Mode: "FM", ReceiveTone: none, TransmitTone: none}
	if tx > 0 {
		v := int64(tx * 1e6)
		c.TransmitHz = &v
	}
	if tone > 0 {
		mhz := int64(tone * 1000)
		c.TransmitTone = domain.Tone{Kind: domain.ToneCTCSS, CTCSSMilliHz: &mhz}
	}
	return c
}

func diffs(c Comparison) string {
	var out []string
	for _, d := range c.Memories {
		s := strings.TrimSpace(fmt.Sprintf("%s:%02d %s", d.Kind, d.Location, strings.Join(d.Fields, "+")))
		if len(d.Candidates) > 0 {
			s += " " + d.Candidates[0].Match + "=" + d.Candidates[0].Channel.Name
		}
		out = append(out, s)
	}
	return strings.Join(out, ", ")
}

// A radio compares with its file memory by memory; decisions bring rflib in
// line, placing memories at their numbers where free.
func TestCompareRadio(t *testing.T) {
	ctx := context.Background()
	s, d := newService(t)
	sh := openShared(t, d)
	wx := sharedChannel(t, sh, "NOAA WX1", 162.55)
	mine := func(c domain.Channel) domain.Channel {
		made, _, err := s.CreateChannel(ctx, c)
		if err != nil {
			t.Fatal(err)
		}
		return made
	}
	calling := mine(analog("Calling", 146.52, 146.52, 0))
	rptr := mine(analog("Repeater", 147.3, 147.9, 131.8))
	extra := mine(analog("Extra", 146.55, 146.55, 0))
	if _, err := s.CreateRadio(ctx, domain.Radio{Name: "HT", Model: "baofeng-uv5rm"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AddChannels(ctx, "HT", AddRequest{IDs: []string{calling.ID, rptr.ID, extra.ID}}); err != nil {
		t.Fatal(err)
	}
	wxMem := wx
	wxMem.Name = "WX1"
	file := radioFile(t, map[int]domain.Channel{
		1: analog("Calling", 146.52, 146.52, 0),
		2: analog("Repeater", 147.3, 147.9, 100), // a different tone
		3: analog("Other", 146.43, 146.43, 0),    // rflib has Extra at 3
		5: wxMem,
		7: analog("Mystery", 146.58, 146.58, 0),
	})
	cmpn, err := s.CompareRadio(ctx, "HT", strings.NewReader(file), CompareOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := diffs(cmpn), "same:01, changed:02 tx tone, radio:03, rflib:03, radio:05 exact=NOAA WX1, radio:07"; got != want {
		t.Fatalf("compare =\n  %s\nwant\n  %s", got, want)
	}

	_, res, err := s.ApplyComparison(ctx, "HT", strings.NewReader(file), CompareDecisions{
		Use: map[int]string{5: wx.ID}, Keep: []int{3, 7}, Take: []int{2},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Used != 1 || res.Kept != 2 || res.Taken != 1 || len(res.AtEnd) != 1 || !strings.Contains(res.AtEnd[0], "Other") {
		t.Fatalf("result = %+v", res)
	}
	got, err := s.lib.GetChannel(ctx, rptr.ID)
	if err != nil || domain.FormatTone(got.TransmitTone) != domain.FormatTone(analog("", 0, 0, 100).TransmitTone) {
		t.Errorf("repeater after take = %+v, %v", got.TransmitTone, err)
	}
	layout, _, err := s.Layout(ctx, "HT")
	if err != nil {
		t.Fatal(err)
	}
	var placed []string
	for _, m := range layout.Memories {
		placed = append(placed, fmt.Sprintf("%02d=%s", m.Location, m.Original.Name))
	}
	if got, want := strings.Join(placed, " "), "01=Calling 02=Repeater 03=Extra 04=Other 05=NOAA WX1 07=Mystery"; got != want {
		t.Errorf("layout = %s, want %s", got, want)
	}
	// Compared again, only what rflib changes remains: Extra, Other moved
	// to the end, and WX1 taking the library channel's name.
	cmpn, err = s.CompareRadio(ctx, "HT", strings.NewReader(file), CompareOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := diffs(cmpn), "same:01, same:02, same:03, rflib:03, changed:05 name, same:07"; got != want {
		t.Errorf("compare again = %s, want %s", got, want)
	}
	// Decisions must fit the memory's kind.
	if _, _, err := s.ApplyComparison(ctx, "HT", strings.NewReader(file), CompareDecisions{Keep: []int{1}}); err == nil || !strings.Contains(err.Error(), "isn't only on the radio") {
		t.Errorf("keep a same memory: %v", err)
	}
}

// A memory's candidates say what differs from it, so a partial match can be
// judged: tones here, and the name unless it's the channel's or a short name.
func TestCandidatesSayWhatDiffers(t *testing.T) {
	rptr := analog("Club Repeater", 147.3, 147.9, 131.8)
	rptr.ID, rptr.ShortNames = "a", []string{"CLUB"}
	other := analog("Other Repeater", 147.3, 147.9, 131.8)
	other.ID = "b"
	got := candidates(analog("CLUB", 147.3, 147.9, 100), []domain.Channel{rptr, other})
	var out []string
	for _, c := range got {
		out = append(out, c.Channel.Name+" "+c.Match+" "+strings.Join(c.Differs, "+"))
	}
	if got, want := strings.Join(out, ", "), "Club Repeater frequencies tx tone, Other Repeater frequencies name+tx tone"; got != want {
		t.Errorf("candidates = %s, want %s", got, want)
	}
	if c := candidates(analog("CLUB", 147.3, 147.9, 131.8), []domain.Channel{rptr}); len(c) != 1 || c[0].Match != "exact" || len(c[0].Differs) != 0 {
		t.Errorf("an exact match = %+v", c)
	}
}

// An empty radio starts from its file: every memory is only on the radio,
// and kept memories keep their numbers, gaps and all.
func TestCompareAdoptsEmptyRadio(t *testing.T) {
	ctx := context.Background()
	s, _ := newService(t)
	if _, err := s.CreateRadio(ctx, domain.Radio{Name: "Mobile", Model: "baofeng-uv5rm"}); err != nil {
		t.Fatal(err)
	}
	file := radioFile(t, map[int]domain.Channel{
		2:  analog("Two", 146.52, 146.52, 0),
		3:  analog("Three", 146.55, 146.55, 0),
		10: analog("Ten", 147.3, 147.9, 131.8),
	})
	cmpn, err := s.CompareRadio(ctx, "Mobile", strings.NewReader(file), CompareOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := diffs(cmpn), "radio:02, radio:03, radio:10"; got != want {
		t.Fatalf("compare = %s, want %s", got, want)
	}
	if _, _, err := s.ApplyComparison(ctx, "Mobile", strings.NewReader(file), CompareDecisions{Keep: []int{2, 3, 10}}); err != nil {
		t.Fatal(err)
	}
	cmpn, err = s.CompareRadio(ctx, "Mobile", strings.NewReader(file), CompareOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := diffs(cmpn), "same:02, same:03, same:10"; got != want {
		t.Errorf("after adopting = %s, want %s", got, want)
	}
}

// Ignoring memory numbers checks only that the radio's channels are in
// rflib somewhere; what's missing joins the end, and rflib's order wins.
func TestCompareIgnoringNumbers(t *testing.T) {
	ctx := context.Background()
	s, _ := newService(t)
	var ids []string
	for _, c := range []domain.Channel{analog("Calling", 146.52, 146.52, 0), analog("Repeater", 147.3, 147.9, 131.8), analog("Extra", 146.55, 146.55, 0)} {
		made, _, err := s.CreateChannel(ctx, c)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, made.ID)
	}
	if _, err := s.CreateRadio(ctx, domain.Radio{Name: "HT", Model: "baofeng-uv5rm"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AddChannels(ctx, "HT", AddRequest{IDs: ids}); err != nil {
		t.Fatal(err)
	}
	file := radioFile(t, map[int]domain.Channel{
		2:  analog("Mystery", 146.58, 146.58, 0),
		7:  analog("Calling", 146.52, 146.52, 0),
		10: analog("Repeater", 147.3, 147.9, 100), // same name and frequency, another tone
	})
	opts := CompareOptions{IgnoreNumbers: true}
	cmpn, err := s.CompareRadio(ctx, "HT", strings.NewReader(file), opts)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := diffs(cmpn), "radio:02, rflib:03, same:07, changed:10 tx tone"; got != want {
		t.Fatalf("compare = %s, want %s", got, want)
	}
	// By number, the same file has nothing at 7 or 10 rflib recognizes.
	byNumber, err := s.CompareRadio(ctx, "HT", strings.NewReader(file), CompareOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := diffs(byNumber), "radio:02, rflib:02, rflib:03, same:07, radio:10"; got != want {
		t.Errorf("by number = %s, want %s", got, want)
	}
	_, res, err := s.ApplyComparison(ctx, "HT", strings.NewReader(file), CompareDecisions{Keep: []int{2}, Options: opts})
	if err != nil {
		t.Fatal(err)
	}
	if res.Kept != 1 || len(res.AtEnd) != 0 {
		t.Errorf("result = %+v", res)
	}
	layout, _, err := s.Layout(ctx, "HT")
	if err != nil {
		t.Fatal(err)
	}
	var placed []string
	for _, m := range layout.Memories {
		placed = append(placed, fmt.Sprintf("%02d=%s", m.Location, m.Original.Name))
	}
	if got, want := strings.Join(placed, " "), "01=Calling 02=Repeater 03=Extra 04=Mystery"; got != want {
		t.Errorf("layout = %s, want %s", got, want)
	}
}

// A memory with the repeater's transmit tone but no receive tone is the
// same channel with another receive tone; and split tones, which the
// FTM-6000 can't hold, are programmed with the transmit tone alone, so a
// radio holding just that compares the same.
func TestComparePairsReceiveToneDifferences(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		rx   int64 // the receive tone rflib has, with tone squelch
		want string
	}{
		{"tone squelch", 131800, "changed:01 rx tone+tone squelch"},
		{"split tones", 146200, "same:01"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := newService(t)
			rptr := analog("Santa Ynez", 145.18, 144.58, 131.8)
			rx := tc.rx
			rptr.ReceiveTone, rptr.ToneSquelch = domain.Tone{Kind: domain.ToneCTCSS, CTCSSMilliHz: &rx}, true
			made, _, err := s.CreateChannel(ctx, rptr)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.CreateRadio(ctx, domain.Radio{Name: "Shack", Model: "yaesu-ftm6000"}); err != nil {
				t.Fatal(err)
			}
			if _, _, err := s.AddChannels(ctx, "Shack", AddRequest{IDs: []string{made.ID}}); err != nil {
				t.Fatal(err)
			}
			// The radio's file, written with the FTM-6000's profile: the
			// transmit tone only.
			mem := analog("Santa Ynez", 145.18, 144.58, 131.8)
			layout, _, err := s.Layout(ctx, "Shack")
			if err != nil {
				t.Fatal(err)
			}
			mem.Name = layout.Memories[0].Channel.Name // as fitted to the display
			layout.Memories[0].Channel = mem
			var buf bytes.Buffer
			if _, err := program.Write(&buf, layout, false, io.Discard); err != nil {
				t.Fatal(err)
			}
			cmpn, err := s.CompareRadio(ctx, "Shack", strings.NewReader(buf.String()), CompareOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if got := diffs(cmpn); got != tc.want {
				t.Fatalf("compare = %s, want %s", got, tc.want)
			}
		})
	}
}

// A memory rflib's programming leaves out is still compared, with no
// number and the reason.
func TestCompareShowsUnprogrammed(t *testing.T) {
	ctx := context.Background()
	s, _ := newService(t)
	cw := analog("Beacon", 144.28, 0, 0)
	cw.Mode = "USB"
	made, _, err := s.CreateChannel(ctx, cw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateRadio(ctx, domain.Radio{Name: "HT", Model: "baofeng-uv5rm"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AddChannels(ctx, "HT", AddRequest{IDs: []string{made.ID}, AnyMode: true}); err != nil {
		t.Fatal(err)
	}
	cmpn, err := s.CompareRadio(ctx, "HT", strings.NewReader(radioFile(t, map[int]domain.Channel{})), CompareOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := diffs(cmpn); got != "rflib:-1" || !strings.Contains(cmpn.Memories[0].Unexported, "doesn't have USB") {
		t.Errorf("compare = %s, %q", got, cmpn.Memories[0].Unexported)
	}
}

// A source's search is taken as it is: a retired channel it matches is
// added (and not programmed), and stays while the search matches it.
func TestSyncAddsRetired(t *testing.T) {
	ctx := context.Background()
	s, d := newService(t)
	sh := openShared(t, d)
	gone := sharedChannel(t, sh, "View Hill", 146.70)
	sharedChannel(t, sh, "Tranquillon", 146.76)
	if _, err := sh.SetStatus(ctx, gone.ID, StatusChange{Status: domain.StatusRetired}, "QRT"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateRadio(ctx, domain.Radio{Name: "HT", Model: "baofeng-uv5rm"}); err != nil {
		t.Fatal(err)
	}
	_, src, err := s.AddSource(ctx, "HT", domain.RadioSource{Query: "band:2m"})
	if err != nil {
		t.Fatal(err)
	}
	sync := func() string {
		if _, err := s.Sync(ctx, "HT", SyncOptions{}); err != nil {
			t.Fatal(err)
		}
		got, _, err := s.radioEntriesNamed(ctx, "HT")
		if err != nil {
			t.Fatal(err)
		}
		return strings.Join(got, ",")
	}
	if got := sync(); got != "Tranquillon,View Hill" {
		t.Errorf("with View Hill retired: %s", got)
	}
	src.Query = "band:2m -status:retired"
	if err := s.UpdateSource(ctx, src); err != nil {
		t.Fatal(err)
	}
	if got := sync(); got != "Tranquillon" {
		t.Errorf("with -status:retired: %s", got)
	}
}

// A copy has the original's sections, sources, channels, and overrides,
// less channels its model can't use; from then on, comparing the two
// offers what one has and the other doesn't.
func TestCopyAndCompareRadios(t *testing.T) {
	ctx := context.Background()
	s, _ := newService(t)
	mine := func(c domain.Channel) domain.Channel {
		made, _, err := s.CreateChannel(ctx, c)
		if err != nil {
			t.Fatal(err)
		}
		return made
	}
	aprs := mine(analog("APRS", 144.39, 0, 0))
	call := mine(analog("Calling", 146.52, 146.52, 0))
	cb := mine(analog("CB 19", 27.185, 27.185, 0))
	gmrs := mine(analog("GMRS 1", 462.5625, 462.5625, 0))
	gmrs.Service = domain.ServiceGMRS
	if _, _, err := s.UpdateChannel(ctx, gmrs); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateRadio(ctx, domain.Radio{Name: "Shack", Model: "chirp-generic", DefaultPower: domain.PowerHigh}); err != nil {
		t.Fatal(err)
	}
	ninety, ten := 90, 10
	if _, err := s.AddSection(ctx, "Shack", domain.RadioSection{Name: "APRS", Start: &ninety, Size: &ten}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AddSource(ctx, "Shack", domain.RadioSource{Query: "service:gmrs"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AddChannels(ctx, "Shack", AddRequest{IDs: []string{aprs.ID}, Section: "aprs"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AddChannels(ctx, "Shack", AddRequest{IDs: []string{call.ID, cb.ID}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Sync(ctx, "Shack", SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, e, err := s.RadioEntry(ctx, "Shack", call.ID); err != nil {
		t.Fatal(err)
	} else {
		e.Name = "CALL"
		if err := s.SetEntry(ctx, e.Section.RadioID, e); err != nil {
			t.Fatal(err)
		}
	}
	layout := func(radio string) string {
		t.Helper()
		l, es, err := s.Layout(ctx, radio)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for i, m := range l.Memories {
			out = append(out, fmt.Sprintf("%d %s %s", m.Location, m.Channel.Name, es[i].Section.Name))
		}
		return strings.Join(out, ", ")
	}
	before := layout("Shack")

	res, err := s.CopyRadio(ctx, "Shack", domain.Radio{Name: "Mobile", Model: "baofeng-uv5rm"})
	if err != nil || res.LayoutErr != nil {
		t.Fatalf("copy: %+v, %v", res, err)
	}
	if res.Copied != 3 || len(res.Skipped) != 1 || res.Skipped[0].Name != "CB 19" || res.Radio.DefaultPower != domain.PowerHigh {
		t.Fatalf("copy result = %+v", res)
	}
	// The same layout, less what the UV-5RM can't receive.
	if got, want := layout("Mobile"), strings.Replace(before, ", 102 CB 19 Other", "", 1); got != want {
		t.Errorf("copy = %s\nwant %s", got, want)
	}
	_, srcs, _, err := s.Sources(ctx, "Mobile")
	if err != nil || len(srcs) != 1 || srcs[0].SectionName != "service:gmrs" {
		t.Errorf("sources = %+v, %v", srcs, err)
	}
	// Independent from now on: Shack gains a channel; comparing offers it.
	extra := mine(analog("Simplex", 146.55, 146.55, 0))
	if _, _, err := s.AddChannels(ctx, "Shack", AddRequest{IDs: []string{extra.ID}, Section: "APRS"}); err != nil {
		t.Fatal(err)
	}
	c, err := s.CompareRadios(ctx, "Mobile", "Shack")
	if err != nil {
		t.Fatal(err)
	}
	if c.Same != 3 || len(c.Missing) != 2 || c.Missing[0].Entry.Channel.Name != "Simplex" || c.Missing[1].Unusable == "" || len(c.Extra) != 0 {
		t.Fatalf("compare = %+v", c)
	}
	if _, res, err := s.AddFromRadio(ctx, "Mobile", "Shack", nil, true); err != nil || len(res.Added) != 1 || layout("Mobile") != strings.Replace(before, ", 102 CB 19 Other", "", 1) {
		t.Fatalf("dry run: %+v, %v", res, err)
	}
	if _, _, err := s.AddFromRadio(ctx, "Mobile", "Shack", []string{cb.ID}, false); err == nil {
		t.Error("added a channel the radio can't use")
	}
	if _, res, err := s.AddFromRadio(ctx, "Mobile", "Shack", nil, false); err != nil || len(res.Added) != 1 || len(res.NewSections) != 0 {
		t.Fatalf("add: %+v, %v", res, err)
	}
	if got := layout("Mobile"); !strings.Contains(got, "91 Simplex APRS") {
		t.Errorf("after adding = %s", got)
	}
	if _, err := s.CompareRadios(ctx, "Mobile", "Mobile"); err == nil {
		t.Error("compared a radio with itself")
	}
}

// Adding from a radio brings the sections the channels need, as the other
// has them, at the end in its order, so nothing here moves; a start that
// doesn't fit is dropped, and channels that would push an existing section
// into a later start go to a section of their own at the end.
func TestAddFromRadioMakesSections(t *testing.T) {
	ctx := context.Background()
	s, _ := newService(t)
	var ids []string
	for i, n := range []string{"A", "B", "C"} {
		made, _, err := s.CreateChannel(ctx, analog(n, 146.52+float64(i)*0.03, 0, 0))
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, made.ID)
	}
	for _, r := range []string{"Full", "Blank"} {
		if _, err := s.CreateRadio(ctx, domain.Radio{Name: r, Model: "baofeng-uv5rm"}); err != nil {
			t.Fatal(err)
		}
	}
	one, two, ten := 1, 2, 10
	for _, r := range []string{"Full", "Blank"} {
		if _, err := s.AddSection(ctx, r, domain.RadioSection{Name: "Mine", Start: &one}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.AddSection(ctx, "Full", domain.RadioSection{Name: "Low", Start: &two}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddSection(ctx, "Full", domain.RadioSection{Name: "High", Start: &ten, Sort: "name"}); err != nil {
		t.Fatal(err)
	}
	for i, sec := range []string{"Low", "High", "High"} {
		if _, _, err := s.AddChannels(ctx, "Full", AddRequest{IDs: []string{ids[i]}, Section: sec}); err != nil {
			t.Fatal(err)
		}
	}
	// Blank holds four channels of its own in Mine, so Low's start at 2
	// can't fit after it; High's at 10 can.
	for i := range 4 {
		made, _, err := s.CreateChannel(ctx, analog(fmt.Sprintf("Own %d", i), 147.0+float64(i)*0.03, 0, 0))
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := s.AddChannels(ctx, "Blank", AddRequest{IDs: []string{made.ID}, Section: "Mine"}); err != nil {
			t.Fatal(err)
		}
	}
	_, res, err := s.AddFromRadio(ctx, "Blank", "Full", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(res.NewSections, ",") != "Low,High" || strings.Join(res.Unstarted, ",") != "Low" || len(res.Added) != 3 {
		t.Fatalf("result = %+v", res)
	}
	_, secs, _ := s.Sections(ctx, "Blank")
	var got []string
	for _, sec := range secs {
		start := "-"
		if sec.Start != nil {
			start = fmt.Sprint(*sec.Start)
		}
		got = append(got, fmt.Sprintf("%s@%s/%s", sec.Name, start, sec.Sort))
	}
	if strings.Join(got, " ") != "Mine@1/ Other@-/ Low@-/ High@10/name" {
		t.Errorf("sections = %s", strings.Join(got, " "))
	}

	// Full gains a channel in Mine; on Blank, Mine (1-4) can't grow into
	// Low's memory 5 once Low starts there, so it goes to its own section.
	five := 5
	if _, err := s.UpdateSection(ctx, "Blank", "Low", SectionEdit{Start: &five}); err != nil {
		t.Fatal(err)
	}
	more, _, err := s.CreateChannel(ctx, analog("D", 146.61, 0, 0))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AddChannels(ctx, "Full", AddRequest{IDs: []string{more.ID}, Section: "Mine"}); err != nil {
		t.Fatal(err)
	}
	if _, res, err = s.AddFromRadio(ctx, "Blank", "Full", nil, false); err != nil || strings.Join(res.Overflowed, ",") != "Mine" || strings.Join(res.NewSections, ",") != "Mine (from Full)" {
		t.Fatalf("overflow: %+v, %v", res, err)
	}
	if l, es, _ := s.Layout(ctx, "Blank"); es[len(es)-1].Section.Name != "Mine (from Full)" || l.Memories[len(es)-1].Location != 12 {
		t.Errorf("last = %+v at %d", es[len(es)-1].Section, l.Memories[len(es)-1].Location)
	}
}

// A move always happens: a section whose start it leaves behind the
// memories before it loses its start and follows the section before it.
func TestMovesDropStartsThatNoLongerFit(t *testing.T) {
	ctx := context.Background()
	s, _ := newService(t)
	if _, err := s.CreateRadio(ctx, domain.Radio{Name: "HT", Model: "baofeng-uv5rm"}); err != nil {
		t.Fatal(err)
	}
	starts := map[string]int{"Amateur": 1, "GMRS": 20, "Public": 50}
	for i, name := range []string{"Amateur", "GMRS", "Public"} {
		n := starts[name]
		if _, err := s.AddSection(ctx, "HT", domain.RadioSection{Name: name, Start: &n}); err != nil {
			t.Fatal(err)
		}
		made, _, err := s.CreateChannel(ctx, analog(name, 146.52+float64(i)*0.03, 0, 0))
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := s.AddChannels(ctx, "HT", AddRequest{IDs: []string{made.ID}, Section: name}); err != nil {
			t.Fatal(err)
		}
	}
	layout := func() string {
		l, es, err := s.Layout(ctx, "HT")
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for i, m := range l.Memories {
			out = append(out, fmt.Sprintf("%s=%d", es[i].Section.Name, m.Location))
		}
		return strings.Join(out, " ")
	}
	// The first section, moved last: its start at 1 goes.
	cleared, err := s.MoveSection(ctx, "HT", "Amateur", 3)
	if err != nil || strings.Join(cleared, ",") != "Amateur" || layout() != "GMRS=20 Public=50 Amateur=51" {
		t.Fatalf("first moved last: %v, %v, %s", cleared, err, layout())
	}
	// A section moved first keeps its start; the ones it passes lose theirs.
	if cleared, err = s.MoveSection(ctx, "HT", "Public", 1); err != nil || strings.Join(cleared, ",") != "GMRS" || layout() != "Public=50 GMRS=51 Amateur=52" {
		t.Fatalf("last moved first: %v, %v, %s", cleared, err, layout())
	}
	// A memory moved into a section that then grows past a later start.
	two := 2
	if _, err := s.UpdateSection(ctx, "HT", "Public", SectionEdit{ClearStart: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateSection(ctx, "HT", "GMRS", SectionEdit{Start: &two}); err != nil {
		t.Fatal(err)
	}
	if layout() != "Public=1 GMRS=2 Amateur=3" {
		t.Fatalf("set up: %s", layout())
	}
	_, es, _ := s.RadioEntries(ctx, "HT")
	if cleared, err = s.MoveChannel(ctx, "HT", es[2].Channel.ID, Move{Section: "Public"}); err != nil || strings.Join(cleared, ",") != "GMRS" || layout() != "Public=1 Public=2 GMRS=3" {
		t.Fatalf("memory moved: %v, %v, %s", cleared, err, layout())
	}
}

// Merging a section moves its channels to the end of another, and its
// sources with them; moving several memories puts them at the end of a
// section in their order.
func TestMergeSectionsAndMoveMany(t *testing.T) {
	ctx := context.Background()
	s, _ := newService(t)
	if _, err := s.CreateRadio(ctx, domain.Radio{Name: "HT", Model: "baofeng-uv5rm"}); err != nil {
		t.Fatal(err)
	}
	ids := map[string]string{}
	for i, name := range []string{"G1", "G2", "G3", "X1", "X2"} {
		made, _, err := s.CreateChannel(ctx, analog(name, 146.52+float64(i)*0.03, 0, 0))
		if err != nil {
			t.Fatal(err)
		}
		ids[name] = made.ID
	}
	twenty := 20
	for _, sec := range []domain.RadioSection{{Name: "GMRS", Start: &twenty}, {Name: "GMRS (from Shack)"}} {
		if _, err := s.AddSection(ctx, "HT", sec); err != nil {
			t.Fatal(err)
		}
	}
	add := func(section string, names ...string) {
		for _, n := range names {
			if _, _, err := s.AddChannels(ctx, "HT", AddRequest{IDs: []string{ids[n]}, Section: section}); err != nil {
				t.Fatal(err)
			}
		}
	}
	add("GMRS", "G1", "G2")
	add("GMRS (from Shack)", "G3")
	add("", "X1", "X2")
	if _, _, err := s.AddSource(ctx, "HT", domain.RadioSource{Query: "name:G", SectionID: "GMRS (from Shack)"}); err != nil {
		t.Fatal(err)
	}
	layout := func() string {
		l, es, err := s.Layout(ctx, "HT")
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for i, m := range l.Memories {
			out = append(out, fmt.Sprintf("%s:%s=%d", es[i].Section.Name, m.Original.Name, m.Location))
		}
		return strings.Join(out, " ")
	}
	moved, cleared, err := s.MergeSection(ctx, "HT", "gmrs (from shack)", "GMRS")
	if err != nil || moved != 1 || len(cleared) != 0 {
		t.Fatalf("merge: %d, %v, %v", moved, cleared, err)
	}
	if got := layout(); got != "GMRS:G1=20 GMRS:G2=21 GMRS:G3=22 Other:X1=23 Other:X2=24" {
		t.Errorf("merged = %s", got)
	}
	if _, srcs, _, _ := s.Sources(ctx, "HT"); srcs[0].SectionName != "GMRS" {
		t.Errorf("source = %+v", srcs[0])
	}
	if _, _, err := s.MergeSection(ctx, "HT", "GMRS", "GMRS"); err == nil {
		t.Error("merged a section into itself")
	}
	// The default merged into GMRS: GMRS becomes the default.
	if _, _, err := s.MergeSection(ctx, "HT", "Other", "GMRS"); err != nil {
		t.Fatal(err)
	}
	if sec, _ := s.Section(ctx, "HT", ""); sec.Name != "GMRS" {
		t.Errorf("default = %s", sec.Name)
	}
	// Several memories at once, in their order, to a new section (which,
	// as new sections do, goes before the default when that is last).
	if _, err := s.AddSection(ctx, "HT", domain.RadioSection{Name: "Mine"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MoveChannels(ctx, "HT", []string{ids["X2"], ids["G1"]}, "Mine"); err != nil {
		t.Fatal(err)
	}
	if got := layout(); got != "Mine:G1=1 Mine:X2=2 GMRS:G2=20 GMRS:G3=21 GMRS:X1=22" {
		t.Errorf("moved many = %s", got)
	}
}

// Narrowing a source's search takes off what it added that no longer
// matches, wherever it now is; channels added by hand stay, and a later
// source can take what it gives up.
func TestSyncRemovesWhatNoLongerMatches(t *testing.T) {
	ctx := context.Background()
	s, _ := newService(t)
	ids := map[string]string{}
	for i, name := range []string{"Near 1", "Near 2", "Far 1", "Far 2", "Hand"} {
		made, _, err := s.CreateChannel(ctx, analog(name, 146.52+float64(i)*0.03, 0, 0))
		if err != nil {
			t.Fatal(err)
		}
		ids[name] = made.ID
	}
	if _, err := s.CreateRadio(ctx, domain.Radio{Name: "HT", Model: "baofeng-uv5rm"}); err != nil {
		t.Fatal(err)
	}
	_, src, err := s.AddSource(ctx, "HT", domain.RadioSource{Query: "name:near OR name:far"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Sync(ctx, "HT", SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AddChannels(ctx, "HT", AddRequest{IDs: []string{ids["Hand"]}, Section: src.SectionID}); err != nil {
		t.Fatal(err)
	}
	// One of the source's channels, dragged to another section.
	if _, err := s.MoveChannel(ctx, "HT", ids["Far 2"], Move{Section: "Other"}); err != nil {
		t.Fatal(err)
	}
	src.Query = "name:near OR name:hand"
	if err := s.UpdateSource(ctx, src); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AddSource(ctx, "HT", domain.RadioSource{Query: "name:far"}); err != nil {
		t.Fatal(err)
	}
	dry, err := s.Sync(ctx, "HT", SyncOptions{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if names(dry.Sources[0].Removed) != "Far 1,Far 2" || names(dry.Sources[1].Add) != "Far 1,Far 2" {
		t.Fatalf("dry run: removed %s; second adds %s", names(dry.Sources[0].Removed), names(dry.Sources[1].Add))
	}
	if _, err := s.Sync(ctx, "HT", SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	got, _, err := s.radioEntriesNamed(ctx, "HT")
	if err != nil || strings.Join(got, ",") != "Near 1,Near 2,Hand,Far 1,Far 2" {
		t.Errorf("after narrowing = %v, %v", got, err)
	}
	// Hand stays even though the narrowed search matches it: it's still
	// hand-added, and so never the source's to remove.
	if _, e, _ := s.RadioEntry(ctx, "HT", ids["Hand"]); e.SourceID != "" {
		t.Errorf("hand-added became the source's: %+v", e)
	}
}

// A channel kept (released from its source) stays when the source stops
// matching it.
func TestReleaseKeepsAChannel(t *testing.T) {
	ctx := context.Background()
	s, _ := newService(t)
	var ids []string
	for i, n := range []string{"A", "B"} {
		made, _, err := s.CreateChannel(ctx, analog(n, 146.52+float64(i)*0.03, 0, 0))
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, made.ID)
	}
	if _, err := s.CreateRadio(ctx, domain.Radio{Name: "HT", Model: "baofeng-uv5rm"}); err != nil {
		t.Fatal(err)
	}
	_, src, err := s.AddSource(ctx, "HT", domain.RadioSource{Query: "name:a OR name:b"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Sync(ctx, "HT", SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, e, _ := s.RadioEntry(ctx, "HT", ids[1]); e.SourceNumber != 1 {
		t.Errorf("source number = %d", e.SourceNumber)
	}
	if n, err := s.ReleaseChannels(ctx, "HT", []string{ids[1]}); err != nil || n != 1 {
		t.Fatalf("release: %d, %v", n, err)
	}
	src.Query = "name:a"
	if err := s.UpdateSource(ctx, src); err != nil {
		t.Fatal(err)
	}
	if res, err := s.Sync(ctx, "HT", SyncOptions{}); err != nil || len(res.Sources[0].Removed) != 0 {
		t.Fatalf("sync: %+v, %v", res, err)
	}
	if got, _, _ := s.radioEntriesNamed(ctx, "HT"); strings.Join(got, ",") != "A,B" {
		t.Errorf("after = %v", got)
	}
	// Removing a source's section removes the source too, and with take
	// off, every channel in it, kept or not.
	res, err := s.RemoveSection(ctx, "HT", src.SectionID, true)
	if err != nil || len(res.Sources) != 1 || res.TakenOff != 2 {
		t.Fatalf("remove section: %+v, %v", res, err)
	}
	if _, srcs, _, _ := s.Sources(ctx, "HT"); len(srcs) != 0 {
		t.Errorf("sources left: %+v", srcs)
	}
	if got, _, _ := s.radioEntriesNamed(ctx, "HT"); len(got) != 0 {
		t.Errorf("after removing the section = %v", got)
	}
}

// Compared with its own export, a radio's power compares too: a memory set
// to another level on the radio is changed, and taking the radio's sets
// this radio's power for it.
func TestComparePower(t *testing.T) {
	ctx := context.Background()
	s, _ := newService(t)
	calling, _, err := s.CreateChannel(ctx, analog("Calling", 146.52, 146.52, 0))
	if err != nil {
		t.Fatal(err)
	}
	rptr, _, err := s.CreateChannel(ctx, analog("Repeater", 147.3, 147.9, 131.8))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateRadio(ctx, domain.Radio{Name: "HT", Model: "baofeng-uv5rm"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AddChannels(ctx, "HT", AddRequest{IDs: []string{calling.ID, rptr.ID}}); err != nil {
		t.Fatal(err)
	}
	layout, _, err := s.Layout(ctx, "HT")
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if _, err := program.Write(&buf, layout, true, io.Discard); err != nil {
		t.Fatal(err)
	}
	// High is 8 W; on the radio, the repeater was set to Low, 1 W, which
	// CHIRP writes as 1.0W.
	lines := strings.Split(buf.String(), "\n")
	for i, l := range lines {
		if strings.HasPrefix(l, "2,") {
			lines[i] = strings.Replace(l, ",8W,", ",1.0W,", 1)
		}
	}
	file := strings.Join(lines, "\n")
	cmpn, err := s.CompareRadio(ctx, "HT", strings.NewReader(file), CompareOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := diffs(cmpn), "same:01, changed:02 power"; got != want {
		t.Fatalf("compare = %s, want %s", got, want)
	}
	if m := cmpn.Memories[1]; m.RadioPower != "1.0W" || m.RflibPower != "8W" {
		t.Errorf("powers = %q, %q", m.RadioPower, m.RflibPower)
	}
	if _, _, err := s.ApplyComparison(ctx, "HT", strings.NewReader(file), CompareDecisions{Take: []int{2}}); err != nil {
		t.Fatal(err)
	}
	r, err := s.GetRadio(ctx, "HT")
	if err != nil {
		t.Fatal(err)
	}
	es, err := s.lib.RadioEntries(ctx, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if es[1].Power != domain.PowerLow {
		t.Errorf("repeater power = %q", es[1].Power)
	}
	if got, err := s.lib.GetChannel(ctx, rptr.ID); err != nil || got.Power != "" {
		t.Errorf("the channel's own power changed: %q, %v", got.Power, err)
	}
	if cmpn, err = s.CompareRadio(ctx, "HT", strings.NewReader(file), CompareOptions{}); err != nil || diffs(cmpn) != "same:01, same:02" {
		t.Errorf("compare again = %s, %v", diffs(cmpn), err)
	}
}
