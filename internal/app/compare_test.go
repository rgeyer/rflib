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

// A source doesn't add a retired channel; once it's active again, sync
// does.
func TestSyncSkipsRetired(t *testing.T) {
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
	if _, _, err := s.AddSource(ctx, "HT", domain.RadioSource{Query: "band:2m"}); err != nil {
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
	if got := sync(); got != "Tranquillon" {
		t.Errorf("with View Hill retired: %s", got)
	}
	if _, err := sh.SetStatus(ctx, gone.ID, StatusChange{Status: domain.StatusActive}, "back"); err != nil {
		t.Fatal(err)
	}
	if got := sync(); got != "Tranquillon,View Hill" {
		t.Errorf("back in service: %s", got)
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

// Adding from a radio onto a nearly blank one brings the sections the
// channels need, as the other has them, in its order; a start that doesn't
// fit here is dropped, and said.
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
	// Blank holds four channels of its own in Mine, which Low follows on
	// Full, so Low's start at 2 can't fit here; High's at 10 can.
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
	if strings.Join(got, " ") != "Mine@1/ Low@-/ High@10/name Other@-/" {
		t.Errorf("sections = %s", strings.Join(got, " "))
	}
}
