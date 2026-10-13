package domain

import (
	"fmt"
	"slices"
	"strings"
)

// Band plans here are United States (FCC) rules. They produce warnings, not
// errors: the library records what you tell it, and a plan can lag the rules.

type band struct{ lowHz, highHz int64 }

// usAmateurBands are the US amateur allocations from 160 m through 23 cm,
// plus the commonly used microwave bands.
var usAmateurBands = []band{
	{1_800_000, 2_000_000}, {3_500_000, 4_000_000}, {5_330_500, 5_406_500}, {7_000_000, 7_300_000},
	{10_100_000, 10_150_000}, {14_000_000, 14_350_000}, {18_068_000, 18_168_000}, {21_000_000, 21_450_000},
	{24_890_000, 24_990_000}, {28_000_000, 29_700_000}, {50_000_000, 54_000_000}, {144_000_000, 148_000_000},
	{219_000_000, 220_000_000}, {222_000_000, 225_000_000}, {420_000_000, 450_000_000}, {902_000_000, 928_000_000},
	{1_240_000_000, 1_300_000_000}, {2_300_000_000, 2_310_000_000}, {2_390_000_000, 2_450_000_000},
	{3_300_000_000, 3_500_000_000}, {5_650_000_000, 5_925_000_000}, {10_000_000_000, 10_500_000_000},
}

func InUSAmateurBand(hz int64) bool {
	return slices.ContainsFunc(usAmateurBands, func(b band) bool { return hz >= b.lowHz && hz <= b.highHz })
}

// FRS/GMRS interstitial and main channels, by FRS channel number.
var (
	FRSChannelsHz = []int64{
		462_562_500, 462_587_500, 462_612_500, 462_637_500, 462_662_500, 462_687_500, 462_712_500, // 1-7
		467_562_500, 467_587_500, 467_612_500, 467_637_500, 467_662_500, 467_687_500, 467_712_500, // 8-14
		462_550_000, 462_575_000, 462_600_000, 462_625_000, 462_650_000, 462_675_000, 462_700_000, 462_725_000, // 15-22
	}
	// GMRSRepeaterInputsHz are the inputs paired with channels 15-22, 5 MHz up.
	GMRSRepeaterInputsHz = []int64{467_550_000, 467_575_000, 467_600_000, 467_625_000, 467_650_000, 467_675_000, 467_700_000, 467_725_000}
	MURSChannelsHz       = []int64{151_820_000, 151_880_000, 151_940_000, 154_570_000, 154_600_000}
	NOAAChannelsHz       = []int64{162_550_000, 162_400_000, 162_475_000, 162_425_000, 162_450_000, 162_500_000, 162_525_000}
	// CBChannelsHz is in channel order; note 23 sits above 24 and 25.
	CBChannelsHz = []int64{
		26_965_000, 26_975_000, 26_985_000, 27_005_000, 27_015_000, 27_025_000, 27_035_000, 27_055_000, 27_065_000, 27_075_000,
		27_085_000, 27_105_000, 27_115_000, 27_125_000, 27_135_000, 27_155_000, 27_165_000, 27_175_000, 27_185_000, 27_205_000,
		27_215_000, 27_225_000, 27_255_000, 27_235_000, 27_245_000, 27_265_000, 27_275_000, 27_285_000, 27_295_000, 27_305_000,
		27_315_000, 27_325_000, 27_335_000, 27_345_000, 27_355_000, 27_365_000, 27_375_000, 27_385_000, 27_395_000, 27_405_000,
	}
)

// PersonalTransmitAllowed reports whether hz is somewhere a ham or
// personal-radio user may transmit: the US amateur bands, FRS and GMRS
// channels and GMRS repeater inputs, MURS, and CB. Radio exports program
// anything else receive-only; see program.Build.
func PersonalTransmitAllowed(hz int64) bool {
	return InUSAmateurBand(hz) || slices.Contains(FRSChannelsHz, hz) || slices.Contains(GMRSRepeaterInputsHz, hz) ||
		slices.Contains(MURSChannelsHz, hz) || slices.Contains(CBChannelsHz, hz)
}

// CheckBandPlan returns warnings where a channel doesn't fit its service's
// US channel plan. Channels without a service are only checked for
// transmitting outside PersonalTransmitAllowed; services without a fixed
// plan (marine, other) are not checked.
func CheckBandPlan(c Channel) []string {
	var warns []string
	warn := func(format string, args ...any) { warns = append(warns, fmt.Sprintf(format, args...)) }
	transmits := c.TransmitHz != nil && !c.RXOnly
	tx := int64(0)
	if transmits {
		tx = *c.TransmitHz
	}
	mhz := FormatFrequencyMHz

	switch c.Service {
	case ServiceUnspecified:
		if transmits && !PersonalTransmitAllowed(tx) {
			warn("transmit frequency %s MHz is outside the amateur bands and FRS, GMRS, MURS, and CB channels; radio exports program it receive-only (set --service, or mark it --rx-only)", mhz(tx))
		}
	case ServiceAmateur:
		if transmits && !InUSAmateurBand(tx) {
			warn("transmit frequency %s MHz is outside the US amateur bands", mhz(tx))
		}
	case ServiceFRS, ServiceMURS, ServiceCB:
		plan := map[Service][]int64{ServiceFRS: FRSChannelsHz, ServiceMURS: MURSChannelsHz, ServiceCB: CBChannelsHz}[c.Service]
		if !slices.Contains(plan, c.ReceiveHz) {
			warn("receive frequency %s MHz is not a %s channel", mhz(c.ReceiveHz), serviceLabel(c.Service))
		}
		if transmits && tx != c.ReceiveHz {
			warn("%s is simplex only; transmit should equal receive", serviceLabel(c.Service))
		}
	case ServiceGMRS:
		if !slices.Contains(FRSChannelsHz, c.ReceiveHz) {
			warn("receive frequency %s MHz is not a GMRS channel", mhz(c.ReceiveHz))
		}
		if transmits && !slices.Contains(FRSChannelsHz, tx) && !slices.Contains(GMRSRepeaterInputsHz, tx) {
			warn("transmit frequency %s MHz is not a GMRS channel or repeater input", mhz(tx))
		}
	case ServiceNOAA:
		if !slices.Contains(NOAAChannelsHz, c.ReceiveHz) {
			warn("receive frequency %s MHz is not a NOAA weather channel", mhz(c.ReceiveHz))
		}
		if transmits {
			warn("NOAA weather radio is receive only; mark the channel --rx-only")
		}
	case ServicePublicSafety:
		if transmits {
			warn("public safety channels are normally receive only; mark the channel --rx-only")
		}
	}
	return warns
}

// serviceLabel names acronym services for messages: "frs" becomes "FRS".
func serviceLabel(s Service) string { return strings.ToUpper(string(s)) }
