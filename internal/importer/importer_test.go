package importer

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ryan/rflib/internal/chirp"
	"github.com/ryan/rflib/internal/domain"
	"github.com/ryan/rflib/internal/export"
	"github.com/ryan/rflib/internal/store"
	"github.com/ryan/rflib/internal/store/sqlite"
)

func newStore(t *testing.T) store.Library {
	t.Helper()
	ctx := context.Background()
	d, err := sqlite.Open(filepath.Join(t.TempDir(), "lib.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := d.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	lib, err := d.GetLibrary(ctx, domain.DefaultLibrary)
	if err != nil {
		t.Fatal(err)
	}
	return d.Library(lib.ID)
}

// seed fills a library with channels covering every field and tone kind.
func seed(t *testing.T, s store.Library) {
	t.Helper()
	ctx := context.Background()
	radius := 40.0
	tone := func(v string) domain.Tone {
		x, err := domain.ParseTone(v)
		if err != nil {
			t.Fatal(err)
		}
		return x
	}
	tx := int64(146340000)
	simplex := int64(146520000)
	for _, c := range []domain.Channel{
		{Name: "Repeater, Main", Location: &domain.Location{Latitude: 45.5152, Longitude: -122.6784, RadiusKM: &radius}, ReceiveHz: 146940000, TransmitHz: &tx, Mode: "FM", Service: domain.ServiceAmateur, ReceiveTone: tone("ctcss:69.3"), TransmitTone: tone("ctcss:69.3"), ToneSquelch: true, Power: domain.PowerHigh, Bandwidth: "25kHz", Tags: []string{"ham", "repeater"}, ShortNames: []string{"Rpt Main", "RptM"}, Notes: "quoted \"note\""},
		{Name: "Calling", ReceiveHz: 146520000, TransmitHz: &simplex, Mode: "NFM", ReceiveTone: tone("none"), TransmitTone: tone("dcs:023:inverted"), Skip: true},
		{Name: "Air", ReceiveHz: 121500000, Mode: "AM", Service: domain.ServiceOther, RXOnly: true, ReceiveTone: tone("dcs:754"), TransmitTone: tone("none")},
	} {
		if _, err := s.CreateChannel(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
}

func exportCSV(t *testing.T, s store.Library) string {
	t.Helper()
	cs, err := s.ListChannels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := (export.GenericCSV{}).Export(&buf, cs); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func exportJSON(t *testing.T, s store.Library) string {
	t.Helper()
	ctx := context.Background()
	cs, err := s.ListChannels(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := export.WriteJSON(&buf, cs, nil); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func mustApply(t *testing.T, s store.Library, b Batch, opts Options) Summary {
	t.Helper()
	if len(b.Errors) > 0 {
		t.Fatalf("parse errors: %v", b.Errors)
	}
	sum, err := Apply(context.Background(), s, b, opts)
	if err != nil {
		t.Fatal(err)
	}
	return sum
}

func TestCSVRoundTripIsLossless(t *testing.T) {
	src := newStore(t)
	seed(t, src)
	first := exportCSV(t, src)

	b, err := ParseGenericCSV(strings.NewReader(first))
	if err != nil {
		t.Fatal(err)
	}
	dst := newStore(t)
	if sum := mustApply(t, dst, b, Options{}); sum.Created != 3 {
		t.Fatalf("summary = %+v", sum)
	}
	if second := exportCSV(t, dst); second != first {
		t.Fatalf("round trip changed CSV:\n--- first\n%s\n--- second\n%s", first, second)
	}
}

func TestJSONRoundTripIsLossless(t *testing.T) {
	src := newStore(t)
	seed(t, src)
	first := exportJSON(t, src)

	b, err := ParseJSON(strings.NewReader(first))
	if err != nil {
		t.Fatal(err)
	}
	dst := newStore(t)
	if sum := mustApply(t, dst, b, Options{}); sum.Created != 3 {
		t.Fatalf("summary = %+v", sum)
	}
	if second := exportJSON(t, dst); second != first {
		t.Fatalf("round trip changed JSON:\n--- first\n%s\n--- second\n%s", first, second)
	}
}

func TestDuplicatePolicies(t *testing.T) {
	s := newStore(t)
	seed(t, s)
	data := exportCSV(t, s)
	parse := func() Batch {
		b, err := ParseGenericCSV(strings.NewReader(strings.ReplaceAll(data, ",high,", ",low,")))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}

	if sum := mustApply(t, s, parse(), Options{OnDuplicate: SkipDuplicates}); sum.Skipped != 3 || sum.Created != 0 {
		t.Fatalf("skip: %+v", sum)
	}
	if sum := mustApply(t, s, parse(), Options{OnDuplicate: UpdateDuplicates}); sum.Updated != 3 {
		t.Fatalf("update: %+v", sum)
	}
	if !strings.Contains(exportCSV(t, s), ",low,") {
		t.Fatal("update did not change power")
	}
	if sum := mustApply(t, s, parse(), Options{OnDuplicate: CreateDuplicates}); sum.Created != 3 {
		t.Fatalf("create: %+v", sum)
	}
	if cs, _ := s.ListChannels(context.Background()); len(cs) != 6 {
		t.Fatalf("channels = %d, want 6", len(cs))
	}
}

func TestDuplicatesMatchEveryProgrammedField(t *testing.T) {
	s := newStore(t)
	csv := "name,receive_mhz,transmit_mhz,receive_tone,transmit_tone,tone_squelch,group_name,power\n" +
		"Calling,146.520,,,,,Metro,\n" +
		"Calling,146.520,,,,,Metro,5W\n" + // duplicate: only power differs
		"Calling,146.520,,,,,,\n" + // duplicate: the old group is only a tag
		"Calling,146.520,146.520,,,,Metro,\n" + // different TX
		"Calling,146.520,,,ctcss:100,,Metro,\n" + // different TX tone
		"Calling,146.520,,ctcss:100,,true,Metro,\n" // different squelch tone
	b, err := ParseGenericCSV(strings.NewReader(csv))
	if err != nil {
		t.Fatal(err)
	}
	if sum := mustApply(t, s, b, Options{}); sum.Created != 4 || sum.Skipped != 2 {
		t.Fatalf("summary = %+v", sum)
	}
}

// A radio's own file shortens names; matching on what is programmed still
// finds the library's channels.
func TestMatchProgrammedIgnoresName(t *testing.T) {
	s := newStore(t)
	library := "name,receive_mhz,transmit_mhz,receive_tone,transmit_tone,tone_squelch,group_name\n" +
		"2m FM Call,146.520,,,,,Calling\n" +
		"Marian,147.300,147.900,,,,Repeaters\n"
	b, err := ParseGenericCSV(strings.NewReader(library))
	if err != nil {
		t.Fatal(err)
	}
	mustApply(t, s, b, Options{})
	radio := "name,receive_mhz,transmit_mhz,receive_tone,transmit_tone,tone_squelch,group_name\n" +
		"VHF Call,146.520,,,,,\n" + // same programming, different name and group
		"MARION HOSP,147.300,147.900,,ctcss:131.8,,\n" // adds a tone: new
	parse := func() Batch {
		b, err := ParseGenericCSV(strings.NewReader(radio))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	if sum := mustApply(t, s, parse(), Options{DryRun: true}); sum.Created != 2 {
		t.Fatalf("exact: %+v", sum)
	}
	if sum := mustApply(t, s, parse(), Options{Match: MatchProgrammed}); sum.Created != 1 || sum.Skipped != 1 {
		t.Fatalf("programmed: %+v", sum)
	}
	if _, err := ParseMatchRule("name"); err == nil {
		t.Fatal("unknown match rule accepted")
	}
}

// Regression: RadioReference exports reuse a name and frequency for sites
// that differ only by tone; both must survive import.
func TestCHIRPSameNameDifferentToneIsNotDuplicate(t *testing.T) {
	s := newStore(t)
	csv := strings.Join(append([]string{strings.Join(chirp.Columns, ",")},
		"57,SBCo Med6,463.125000,split,468.125000,TSQL,110.9,110.9,023,NN,023,Tone->Tone,FM,5.00,,50W,Cottage Santa Ynez,,,,",
		"58,SBCo Med6,463.125000,split,468.125000,TSQL,114.8,114.8,023,NN,023,Tone->Tone,FM,5.00,,50W,Cottage Goleta,,,,",
	), "\r\n")
	b, err := ParseCHIRP(strings.NewReader(csv))
	if err != nil {
		t.Fatal(err)
	}
	if sum := mustApply(t, s, b, Options{}); sum.Created != 2 || sum.Skipped != 0 {
		t.Fatalf("summary = %+v", sum)
	}
	// Re-importing the same file is still a no-op.
	if sum := mustApply(t, s, b, Options{}); sum.Created != 0 || sum.Skipped != 2 {
		t.Fatalf("re-import summary = %+v", sum)
	}
}

func TestDryRunWritesNothing(t *testing.T) {
	s := newStore(t)
	b, err := ParseGenericCSV(strings.NewReader("name,receive_mhz\nCalling,146.520\n"))
	if err != nil {
		t.Fatal(err)
	}
	if sum := mustApply(t, s, b, Options{DryRun: true}); sum.Created != 1 {
		t.Fatalf("summary = %+v", sum)
	}
	if cs, _ := s.ListChannels(context.Background()); len(cs) != 0 {
		t.Fatalf("dry run wrote %d channels", len(cs))
	}
}

func TestApplyIsAtomic(t *testing.T) {
	s := newStore(t)
	// The second record has a mode the store refuses.
	b := Batch{Records: []Record{
		{Row: 2, Channel: domain.Channel{Name: "ok", ReceiveHz: 146520000, Mode: "FM", ReceiveTone: domain.Tone{Kind: domain.ToneNone}, TransmitTone: domain.Tone{Kind: domain.ToneNone}}},
		{Row: 3, Channel: domain.Channel{Name: "bad", ReceiveHz: 146550000, Mode: "DMR", ReceiveTone: domain.Tone{Kind: domain.ToneNone}, TransmitTone: domain.Tone{Kind: domain.ToneNone}}},
	}}
	_, err := Apply(context.Background(), s, b, Options{})
	if err == nil || !strings.Contains(err.Error(), "row 3") {
		t.Fatalf("err = %v", err)
	}
	if cs, _ := s.ListChannels(context.Background()); len(cs) != 0 {
		t.Fatalf("failed import left %d channels", len(cs))
	}
}

// Files from before groups became tags and locations import as if the
// library had been migrated: the group's name is a tag, and its location the
// channel's.
func TestOldGroupsBecomeTagsAndLocations(t *testing.T) {
	s := newStore(t)
	b, err := ParseGenericCSV(strings.NewReader("name,receive_mhz,group_id,group_name,group_latitude,group_longitude,tags\n" +
		"Calling,146.520,g1,Santa Barbara,34.42,-119.7,ham\n" +
		"Own,146.550,g1,Santa Barbara,34.42,-119.7,\n"))
	if err != nil || len(b.Errors) > 0 {
		t.Fatal(err, b.Errors)
	}
	mustApply(t, s, b, Options{Tags: []string{"Imported"}})
	cs, _ := s.ListChannels(context.Background())
	if len(cs) != 2 || strings.Join(cs[0].Tags, ",") != "ham,imported,santa barbara" || cs[0].Location == nil || cs[0].Location.Latitude != 34.42 {
		t.Fatalf("CSV channel = %+v", cs[0])
	}

	b, err = ParseJSON(strings.NewReader(`{"format":"rflib","version":1,
		"groups":[{"id":"g1","name":"Metro","latitude":45.5,"longitude":-122.6,"radius_km":30},{"id":"g2","name":"Statewide"}],
		"channels":[
			{"id":"a","group_id":"g1","name":"A","receive_hz":146940000,"mode":"FM","receive_tone":"none","transmit_tone":"none","tags":[]},
			{"id":"b","group_id":"g2","name":"B","receive_hz":146970000,"mode":"FM","receive_tone":"none","transmit_tone":"none","tags":[]}]}`))
	if err != nil || len(b.Errors) > 0 {
		t.Fatal(err, b.Errors)
	}
	a, bb := b.Records[0].Channel, b.Records[1].Channel
	if strings.Join(a.Tags, ",") != "Metro" || a.Location == nil || *a.Location.RadiusKM != 30 || strings.Join(bb.Tags, ",") != "Statewide" || bb.Location != nil {
		t.Fatalf("v1 backup channels = %+v, %+v", a, bb)
	}
}

func TestParseCSVReportsRowErrors(t *testing.T) {
	csv := "\xef\xbb\xbfName,Receive_MHz,Mode,Receive_Tone,Tone_Squelch\n" +
		"Good,146.520,,,\n" +
		"BadFreq,abc,FM,,\n" +
		"BadTone,146.520,FM,ctcss:10,\n" +
		"BadMode,146.520,DMR,,\n" +
		"Squelch,146.520,FM,none,true\n"
	b, err := ParseGenericCSV(strings.NewReader(csv))
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Records) != 1 || b.Records[0].Channel.Mode != "FM" {
		t.Fatalf("records = %+v", b.Records)
	}
	var rows []int
	for _, e := range b.Errors {
		rows = append(rows, e.Row)
	}
	if len(rows) != 4 || rows[0] != 3 || rows[3] != 6 {
		t.Fatalf("error rows = %v (%v)", rows, b.Errors)
	}
}

func TestParseCSVRequiresColumns(t *testing.T) {
	if _, err := ParseGenericCSV(strings.NewReader("name,frequency\n")); err == nil || !strings.Contains(err.Error(), "receive_mhz") {
		t.Fatalf("err = %v", err)
	}
}

func TestParseJSONRejectsOtherFormats(t *testing.T) {
	if _, err := ParseJSON(strings.NewReader(`{"format":"other","version":1}`)); err == nil {
		t.Fatal("expected unsupported format error")
	}
	b, err := ParseJSON(strings.NewReader(`{"format":"rflib","version":1,"groups":[],"channels":[{"id":"x","group_id":"nope","name":"a","receive_hz":1,"mode":"FM","receive_tone":"none","transmit_tone":"none"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Errors) != 1 || !strings.Contains(b.Errors[0].Error(), "not in the backup") {
		t.Fatalf("errors = %v", b.Errors)
	}
}

func TestServiceOverrideAndWarnings(t *testing.T) {
	s := newStore(t)
	// A RadioReference-style public safety repeater with a known input.
	csv := "name,receive_mhz,transmit_mhz\nDispatch,460.275,465.275\nCalling,146.520,146.520\n"
	b, err := ParseGenericCSV(strings.NewReader(csv))
	if err != nil {
		t.Fatal(err)
	}
	sum := mustApply(t, s, b, Options{Service: domain.ServicePublicSafety})
	if len(sum.Warnings) != 0 {
		t.Fatalf("public safety defaults to receive only, so no warnings expected: %v", sum.Warnings)
	}
	all, _ := s.ListChannels(context.Background())
	var cs []domain.Channel
	for _, c := range all {
		if c.Service == domain.ServicePublicSafety {
			cs = append(cs, c)
		}
	}
	if len(cs) != 2 || !cs[0].RXOnly || cs[0].TransmitHz == nil {
		t.Fatalf("channels = %+v", cs)
	}

	// Relabelling with update: service and rx_only aren't part of the
	// duplicate key, so the same rows update in place.
	transmit := false
	sum = mustApply(t, s, b, Options{OnDuplicate: UpdateDuplicates, Service: domain.ServiceAmateur, RXOnly: &transmit})
	if sum.Updated != 2 || len(sum.Warnings) != 1 || sum.Warnings[0].Row != 2 || !strings.Contains(sum.Warnings[0].Error(), "Dispatch") {
		t.Fatalf("summary = %+v", sum)
	}
}

// A grid column locates a channel at the square's center, reaching its
// corners unless radius_km is given.
func TestCSVGrid(t *testing.T) {
	csv := "name,receive_mhz,grid,latitude,longitude,radius_km\n" +
		"Square,146.52,CM94sq,,,\n" +
		"Sized,146.55,cm94,,,2\n" +
		"Both,146.58,CM94,34,-120,\n" +
		"Bad,146.61,CM9,,,\n"
	b, err := ParseGenericCSV(strings.NewReader(csv))
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Records) != 2 || len(b.Errors) != 2 {
		t.Fatalf("records %d, errors %v", len(b.Records), b.Errors)
	}
	sq, sized := b.Records[0].Channel.Location, b.Records[1].Channel.Location
	if sq == nil || sq.Latitude != 34.6875 || sq.Longitude != -120.458333 || sq.RadiusKM == nil || *sq.RadiusKM < 4 || *sq.RadiusKM > 5 {
		t.Errorf("CM94sq = %+v", sq)
	}
	if sized == nil || sized.Latitude != 34.5 || sized.Longitude != -121 || *sized.RadiusKM != 2 {
		t.Errorf("CM94 with a radius = %+v", sized)
	}
}
