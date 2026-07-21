package export

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ryan/rflib/internal/domain"
)

func TestGenericCSV(t *testing.T) {
	tone, _ := domain.ParseTone("ctcss:100.0")
	var out bytes.Buffer
	err := (GenericCSV{}).Export(&out, []domain.Channel{{ID: "1", Name: "Local, Repeater", ReceiveHz: 146940000, Mode: "FM", ReceiveTone: tone, TransmitTone: tone, Tags: []string{"ham", "local"}}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"Local, Repeater"`) || !strings.Contains(out.String(), "ctcss:100.000") {
		t.Fatalf("unexpected CSV: %s", out.String())
	}
}
