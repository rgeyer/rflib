package query

import (
	"strings"
	"testing"

	"github.com/ryan/rflib/internal/domain"
)

func TestTagRepeats(t *testing.T) {
	tx := int64(147_600_000)
	repeater := domain.Channel{Name: "Club", ReceiveHz: 147_000_000, TransmitHz: &tx, Mode: "FM", Service: domain.ServiceAmateur,
		Tags: []string{"ham", "repeater", "2m", "fm", "club", "70cm", "cb"}}
	got := strings.Join(TagRepeats(repeater), "\n")
	for _, want := range []string{
		`tag "ham" repeats one of the channel's fields: service:amateur finds it`,
		`tag "repeater" repeats one of the channel's fields: is:repeater finds it`,
		`tag "2m" repeats one of the channel's fields: band:2m finds it`,
		`tag "fm" repeats one of the channel's fields: mode:fm finds it`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	// A tag naming some other channel's field value repeats nothing here.
	for _, tag := range []string{"club", "70cm", "cb"} {
		if strings.Contains(got, `"`+tag+`"`) {
			t.Errorf("%s flagged:\n%s", tag, got)
		}
	}
	weather := domain.Channel{Name: "WX1", ReceiveHz: 162_550_000, Mode: "FM", Service: domain.ServiceNOAA, RXOnly: true, Tags: []string{"weather", "noaa"}}
	if n := len(TagRepeats(weather)); n != 2 {
		t.Errorf("weather tags = %q", TagRepeats(weather))
	}
}

// Shared tags never repeat a field, on any channel.
func TestCuratedTagsAreNotFieldValues(t *testing.T) {
	for _, c := range domain.CuratedTags {
		if IsFieldValue(c.Name) {
			t.Errorf("shared tag %q is a field value; search finds it without the tag", c.Name)
		}
	}
	if !IsFieldValue("simplex") || !IsFieldValue("gmrs") || IsFieldValue("calling") {
		t.Error("IsFieldValue is wrong for simplex, gmrs, or calling")
	}
}
