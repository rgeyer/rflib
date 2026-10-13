package importer

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/ryan/rflib/internal/domain"
	"github.com/ryan/rflib/internal/export"
	"github.com/ryan/rflib/internal/store"
)

func TestParseCHIRPGoldenExportRoundTrips(t *testing.T) {
	data, err := os.ReadFile("../export/testdata/chirp.csv")
	if err != nil {
		t.Fatal(err)
	}
	b, err := ParseCHIRP(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Errors) != 0 || len(b.Records) != 7 {
		t.Fatalf("records = %d, errors = %v", len(b.Records), b.Errors)
	}
	s := newStore(t)
	mustApply(t, s, b, Options{})
	cs, err := s.ListChannels(context.Background(), store.ChannelFilter{})
	if err != nil {
		t.Fatal(err)
	}
	// Store order is by name; CHIRP order is by location. Re-export in the
	// original order and compare byte for byte.
	order := map[string]int{}
	for _, r := range b.Records {
		order[r.Channel.Name] = r.Row
	}
	sorted := make([]domain.Channel, len(cs))
	for _, c := range cs {
		sorted[order[c.Name]-2] = c
	}
	var out bytes.Buffer
	if err := (export.CHIRP{Start: 1, MaxNameLen: 12}).Export(&out, sorted); err != nil {
		t.Fatal(err)
	}
	if out.String() != string(data) {
		t.Fatalf("re-export differs:\n%s", out.String())
	}
}

func TestParseCHIRPVariants(t *testing.T) {
	// An older CHIRP file: LF endings, no RxDtcsCode, lowercase mode, and an
	// empty memory row.
	csv := "Location,Name,Frequency,Duplex,Offset,Tone,rToneFreq,cToneFreq,DtcsCode,DtcsPolarity,Mode,TStep,Skip,Comment\n" +
		"0,,146.520000,,0.000000,,88.5,88.5,023,NN,fm,5.00,,\n" +
		"1,DCS Rpt,442.000000,+,5.000000,DTCS,88.5,88.5,754,RN,FM,5.00,P,Club\n" +
		"2,,,,,,,,,,,,,\n" +
		"3,DV Rpt,145.000000,-,0.600000,,88.5,88.5,023,NN,DV,5.00,,\n" +
		"4,Bad Tone,146.000000,,0.000000,TSQL,88.5,12.0,023,NN,FM,5.00,,\n"
	b, err := ParseCHIRP(strings.NewReader(csv))
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Records) != 2 {
		t.Fatalf("records = %+v", b.Records)
	}
	if b.Records[0].Channel.Name != "146.520000" || b.Records[0].Channel.Mode != "FM" {
		t.Errorf("unnamed memory = %+v", b.Records[0].Channel)
	}
	rpt := b.Records[1].Channel
	if *rpt.TransmitHz != 447000000 || domain.FormatTone(rpt.TransmitTone) != "dcs:754:inverted" || domain.FormatTone(rpt.ReceiveTone) != "dcs:754" || !rpt.ToneSquelch || !rpt.Skip || rpt.Notes != "Club" {
		t.Errorf("DTCS repeater = %+v", rpt)
	}
	if len(b.Errors) != 2 || b.Errors[0].Row != 5 || !strings.Contains(b.Errors[0].Error(), "DV") || b.Errors[1].Row != 6 {
		t.Errorf("errors = %v", b.Errors)
	}
}

func TestParseCHIRPRequiresFrequency(t *testing.T) {
	if _, err := ParseCHIRP(strings.NewReader("Location,Name\n")); err == nil {
		t.Fatal("accepted a CSV without a Frequency column")
	}
}
