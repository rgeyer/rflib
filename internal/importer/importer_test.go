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
)

func newStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "lib.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return s
}

// seed fills a library with channels covering every field and tone kind.
func seed(t *testing.T, s *store.Store) {
	t.Helper()
	ctx := context.Background()
	lat, lon, radius := 45.5152, -122.6784, 40.0
	metro, err := s.CreateGroup(ctx, domain.Group{Name: "Metro", Description: "Local", Latitude: &lat, Longitude: &lon, RadiusKM: &radius, Notes: "n"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateGroup(ctx, domain.Group{Name: "Empty"}); err != nil {
		t.Fatal(err)
	}
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
		{Name: "Repeater, Main", GroupID: &metro.ID, ReceiveHz: 146940000, TransmitHz: &tx, Mode: "FM", Service: domain.ServiceAmateur, ReceiveTone: tone("ctcss:69.3"), TransmitTone: tone("ctcss:69.3"), ToneSquelch: true, Power: domain.PowerHigh, Bandwidth: "25kHz", Tags: []string{"ham", "repeater"}, ShortNames: []string{"Rpt Main", "RptM"}, Notes: "quoted \"note\""},
		{Name: "Calling", ReceiveHz: 146520000, TransmitHz: &simplex, Mode: "NFM", ReceiveTone: tone("none"), TransmitTone: tone("dcs:023:inverted"), Skip: true},
		{Name: "Air", ReceiveHz: 121500000, Mode: "AM", Service: domain.ServiceOther, RXOnly: true, ReceiveTone: tone("dcs:754"), TransmitTone: tone("none")},
	} {
		if _, err := s.CreateChannel(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
}

func exportCSV(t *testing.T, s *store.Store) string {
	t.Helper()
	cs, err := s.ListChannels(context.Background(), store.ChannelFilter{})
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := (export.GenericCSV{}).Export(&buf, cs); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func exportJSON(t *testing.T, s *store.Store) string {
	t.Helper()
	ctx := context.Background()
	cs, err := s.ListChannels(ctx, store.ChannelFilter{})
	if err != nil {
		t.Fatal(err)
	}
	gs, err := s.ListGroups(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := export.WriteJSON(&buf, gs, cs); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func mustApply(t *testing.T, s *store.Store, b Batch, opts Options) Summary {
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
	if sum := mustApply(t, dst, b, Options{}); sum.Created != 3 || len(sum.GroupsCreated) != 1 {
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
	if sum := mustApply(t, dst, b, Options{}); sum.Created != 3 || len(sum.GroupsCreated) != 2 {
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
	if cs, _ := s.ListChannels(context.Background(), store.ChannelFilter{}); len(cs) != 6 {
		t.Fatalf("channels = %d, want 6", len(cs))
	}
}

func TestDuplicatesMatchEveryProgrammedField(t *testing.T) {
	s := newStore(t)
	csv := "name,receive_mhz,transmit_mhz,receive_tone,transmit_tone,tone_squelch,group_name,power\n" +
		"Calling,146.520,,,,,Metro,\n" +
		"Calling,146.520,,,,,Metro,5W\n" + // duplicate: only power differs
		"Calling,146.520,,,,,,\n" + // different group
		"Calling,146.520,146.520,,,,Metro,\n" + // different TX
		"Calling,146.520,,,ctcss:100,,Metro,\n" + // different TX tone
		"Calling,146.520,,ctcss:100,,true,Metro,\n" // different squelch tone
	b, err := ParseGenericCSV(strings.NewReader(csv))
	if err != nil {
		t.Fatal(err)
	}
	if sum := mustApply(t, s, b, Options{}); sum.Created != 5 || sum.Skipped != 1 {
		t.Fatalf("summary = %+v", sum)
	}
}

// A radio's own file shortens names and has no groups; matching on what is
// programmed still finds the library's channels.
func TestMatchProgrammedIgnoresNameAndGroup(t *testing.T) {
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
	b, err := ParseGenericCSV(strings.NewReader("name,receive_mhz,group_name\nCalling,146.520,New Group\n"))
	if err != nil {
		t.Fatal(err)
	}
	sum := mustApply(t, s, b, Options{DryRun: true})
	if sum.Created != 1 || len(sum.GroupsCreated) != 1 {
		t.Fatalf("summary = %+v", sum)
	}
	cs, _ := s.ListChannels(context.Background(), store.ChannelFilter{})
	gs, _ := s.ListGroups(context.Background())
	if len(cs) != 0 || len(gs) != 0 {
		t.Fatalf("dry run wrote %d channels, %d groups", len(cs), len(gs))
	}
}

func TestApplyIsAtomic(t *testing.T) {
	s := newStore(t)
	// The second record names a group by ID only, which can't be created.
	b := Batch{Records: []Record{
		{Row: 2, Channel: domain.Channel{Name: "ok", ReceiveHz: 146520000, Mode: "FM", ReceiveTone: domain.Tone{Kind: domain.ToneNone}, TransmitTone: domain.Tone{Kind: domain.ToneNone}}},
		{Row: 3, Channel: domain.Channel{Name: "bad", ReceiveHz: 146550000, Mode: "FM", ReceiveTone: domain.Tone{Kind: domain.ToneNone}, TransmitTone: domain.Tone{Kind: domain.ToneNone}, Group: &domain.Group{ID: "missing"}}},
	}}
	_, err := Apply(context.Background(), s, b, Options{})
	if err == nil || !strings.Contains(err.Error(), "row 3") {
		t.Fatalf("err = %v", err)
	}
	if cs, _ := s.ListChannels(context.Background(), store.ChannelFilter{}); len(cs) != 0 {
		t.Fatalf("failed import left %d channels", len(cs))
	}
}

func TestGroupAndTagOverrides(t *testing.T) {
	s := newStore(t)
	g, err := s.CreateGroup(context.Background(), domain.Group{Name: "Target"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := ParseGenericCSV(strings.NewReader("name,receive_mhz,group_name,tags\nCalling,146.520,Elsewhere,ham\n"))
	if err != nil {
		t.Fatal(err)
	}
	if sum := mustApply(t, s, b, Options{Group: "Target", Tags: []string{"Imported"}}); len(sum.GroupsCreated) != 0 {
		t.Fatalf("override still created groups: %v", sum.GroupsCreated)
	}
	cs, _ := s.ListChannels(context.Background(), store.ChannelFilter{})
	if len(cs) != 1 || *cs[0].GroupID != g.ID || strings.Join(cs[0].Tags, ",") != "ham,imported" {
		t.Fatalf("channel = %+v", cs[0])
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
	cs, _ := s.ListChannels(context.Background(), store.ChannelFilter{Service: domain.ServicePublicSafety})
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
