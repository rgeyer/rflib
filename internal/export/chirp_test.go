package export

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ryan/rflib/internal/domain"
)

var update = flag.Bool("update", false, "rewrite golden files")

func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test ./internal/export -update to create it)", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("output differs from %s:\n--- got\n%s\n--- want\n%s", path, got, want)
	}
}

func mustTone(t *testing.T, v string) domain.Tone {
	t.Helper()
	x, err := domain.ParseTone(v)
	if err != nil {
		t.Fatal(err)
	}
	return x
}

func TestCHIRPGolden(t *testing.T) {
	mhz := func(v int64) *int64 { return &v }
	none := mustTone(t, "none")
	channels := []domain.Channel{
		{Name: "Calling", ReceiveHz: 146520000, TransmitHz: mhz(146520000), Mode: "FM", ReceiveTone: none, TransmitTone: none},
		{Name: "Metro Repeater", ReceiveHz: 146940000, TransmitHz: mhz(146340000), Mode: "FM", ReceiveTone: mustTone(t, "ctcss:100"), TransmitTone: mustTone(t, "ctcss:100"), ToneSquelch: true, Power: "50W", Notes: "Linked, \"busy\""},
		{Name: "UHF Repeater", ReceiveHz: 444100000, TransmitHz: mhz(449100000), Mode: "FM", ReceiveTone: none, TransmitTone: mustTone(t, "ctcss:107.2")},
		{Name: "DCS Simplex", ReceiveHz: 446000000, TransmitHz: mhz(446000000), Mode: "NFM", ReceiveTone: mustTone(t, "dcs:023"), TransmitTone: mustTone(t, "dcs:023"), ToneSquelch: true},
		{Name: "Cross Band", ReceiveHz: 146520000, TransmitHz: mhz(446000000), Mode: "FM", ReceiveTone: mustTone(t, "dcs:754"), TransmitTone: mustTone(t, "ctcss:88.5"), ToneSquelch: true},
		{Name: "NOAA WX1", ReceiveHz: 162550000, Mode: "FM", ReceiveTone: none, TransmitTone: none, Skip: true},
		{Name: "Tower", ReceiveHz: 118300000, Mode: "AM", ReceiveTone: none, TransmitTone: none},
	}
	var out bytes.Buffer
	if err := (CHIRP{Start: 1, MaxNameLen: 12}).Export(&out, channels); err != nil {
		t.Fatal(err)
	}
	golden(t, "chirp.csv", out.Bytes())
}

func TestCHIRPRejectsAllUnsupportedChannels(t *testing.T) {
	none := mustTone(t, "none")
	channels := []domain.Channel{
		{Name: "Odd Tone", ReceiveHz: 146520000, Mode: "FM", ReceiveTone: none, TransmitTone: mustTone(t, "ctcss:100.5")},
		{Name: "Fine", ReceiveHz: 146550000, Mode: "FM", ReceiveTone: none, TransmitTone: none},
		{Name: "Odd DCS", ReceiveHz: 146580000, Mode: "FM", ReceiveTone: mustTone(t, "dcs:024"), TransmitTone: none, ToneSquelch: true},
	}
	var out bytes.Buffer
	err := (CHIRP{}).Export(&out, channels)
	if err == nil || !strings.Contains(err.Error(), "Odd Tone") || !strings.Contains(err.Error(), "Odd DCS") {
		t.Fatalf("err = %v", err)
	}
	if out.Len() != 0 {
		t.Fatal("failed export wrote output")
	}
}
