package domain

import (
	"strings"
	"testing"
)

func TestCheckBandPlan(t *testing.T) {
	hz := func(v int64) *int64 { return &v }
	for _, tc := range []struct {
		name string
		c    Channel
		want string // substring of the only warning; empty means none
	}{
		{"ham in band", Channel{Service: ServiceAmateur, ReceiveHz: 146_940_000, TransmitHz: hz(146_340_000)}, ""},
		{"ham tx out of band", Channel{Service: ServiceAmateur, ReceiveHz: 146_520_000, TransmitHz: hz(151_820_000)}, "outside the US amateur bands"},
		{"ham rx only out of band", Channel{Service: ServiceAmateur, ReceiveHz: 162_550_000}, ""},
		{"gmrs repeater", Channel{Service: ServiceGMRS, ReceiveHz: 462_550_000, TransmitHz: hz(467_550_000)}, ""},
		{"gmrs bad tx", Channel{Service: ServiceGMRS, ReceiveHz: 462_550_000, TransmitHz: hz(467_500_000)}, "not a GMRS channel or repeater input"},
		{"frs repeater", Channel{Service: ServiceFRS, ReceiveHz: 462_550_000, TransmitHz: hz(467_550_000)}, "FRS is simplex only"},
		{"frs off plan", Channel{Service: ServiceFRS, ReceiveHz: 462_500_000}, "not a FRS channel"},
		{"cb", Channel{Service: ServiceCB, ReceiveHz: 27_185_000, TransmitHz: hz(27_185_000)}, ""},
		{"noaa transmitting", Channel{Service: ServiceNOAA, ReceiveHz: 162_550_000, TransmitHz: hz(162_550_000)}, "receive only"},
		{"noaa rx only flag", Channel{Service: ServiceNOAA, ReceiveHz: 162_550_000, TransmitHz: hz(162_550_000), RXOnly: true}, ""},
		{"public safety transmitting", Channel{Service: ServicePublicSafety, ReceiveHz: 460_275_000, TransmitHz: hz(465_275_000)}, "--rx-only"},
		{"unspecified receive only", Channel{ReceiveHz: 460_275_000}, ""},
		{"unspecified ham repeater", Channel{ReceiveHz: 147_300_000, TransmitHz: hz(147_900_000)}, ""},
		{"unspecified gmrs input", Channel{ReceiveHz: 462_550_000, TransmitHz: hz(467_550_000)}, ""},
		{"unspecified public safety transmitting", Channel{ReceiveHz: 460_275_000, TransmitHz: hz(465_275_000)}, "receive-only"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := CheckBandPlan(tc.c)
			switch {
			case tc.want == "" && len(got) != 0:
				t.Fatalf("unexpected warnings %v", got)
			case tc.want != "" && (len(got) != 1 || !strings.Contains(got[0], tc.want)):
				t.Fatalf("warnings %v, want one containing %q", got, tc.want)
			}
		})
	}
}

func TestParseService(t *testing.T) {
	if s, err := ParseService(" GMRS "); err != nil || s != ServiceGMRS {
		t.Fatalf("got %q, %v", s, err)
	}
	if s, err := ParseService(""); err != nil || s != ServiceUnspecified {
		t.Fatalf("got %q, %v", s, err)
	}
	if _, err := ParseService("ham"); err == nil {
		t.Fatal("accepted unknown service")
	}
	if !DefaultRXOnly(ServiceNOAA) || !DefaultRXOnly(ServicePublicSafety) || DefaultRXOnly(ServiceAmateur) {
		t.Fatal("DefaultRXOnly is wrong")
	}
}
