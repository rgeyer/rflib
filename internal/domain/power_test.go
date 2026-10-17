package domain

import (
	"testing"
)

func TestPowerFromText(t *testing.T) {
	for in, want := range map[string]PowerIntent{
		"HIGH": PowerHigh, " mid ": PowerMedium, "Mid1": PowerMedium, "L": PowerLow, "max": PowerMax,
		"50W": PowerUnset, "5.0W": PowerUnset, "": PowerUnset, "turbo": PowerUnset,
	} {
		if got := PowerFromText(in); got != want {
			t.Errorf("PowerFromText(%q) = %q, want %q", in, got, want)
		}
	}
	if _, err := ParsePowerIntent("50W"); err == nil {
		t.Error("ParsePowerIntent accepted watts")
	}
	if got := ResolvePower(PowerUnset, PowerLow, PowerHigh); got != PowerLow {
		t.Errorf("ResolvePower = %q", got)
	}
}

func TestServiceCapWatts(t *testing.T) {
	hz := func(v int64) *int64 { return &v }
	for _, tc := range []struct {
		c    Channel
		want float64
		ok   bool
	}{
		{Channel{Service: ServiceFRS, TransmitHz: hz(462_562_500)}, 2, true},
		{Channel{Service: ServiceFRS, TransmitHz: hz(467_637_500)}, 0.5, true},
		{Channel{Service: ServiceGMRS, TransmitHz: hz(462_612_500)}, 5, true},
		{Channel{Service: ServiceGMRS, TransmitHz: hz(467_712_500)}, 0.5, true},
		{Channel{Service: ServiceGMRS, TransmitHz: hz(467_550_000)}, 50, true},
		{Channel{Service: ServiceMURS, TransmitHz: hz(151_820_000)}, 2, true},
		{Channel{Service: ServiceCB, TransmitHz: hz(27_185_000)}, 4, true},
		{Channel{Service: ServiceCB, TransmitHz: hz(27_185_000), Mode: "LSB"}, 12, true},
		{Channel{Service: ServiceAmateur, TransmitHz: hz(146_520_000)}, 0, false},
		{Channel{Service: ServiceGMRS, TransmitHz: hz(462_550_000), RXOnly: true}, 0, false},
		{Channel{Service: ServiceGMRS}, 0, false},
	} {
		got, ok := ServiceCapWatts(tc.c)
		if got != tc.want || ok != tc.ok {
			t.Errorf("%s at %v: got %v, %v; want %v, %v", tc.c.Service, tc.c.TransmitHz, got, ok, tc.want, tc.ok)
		}
	}
}
