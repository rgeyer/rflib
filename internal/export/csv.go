package export

import (
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/ryan/rflib/internal/domain"
)

type Exporter interface {
	Export(io.Writer, []domain.Channel) error
}

type GenericCSV struct{}

func (GenericCSV) Export(w io.Writer, channels []domain.Channel) error {
	csvw := csv.NewWriter(w)
	if err := csvw.Write([]string{"id", "name", "receive_mhz", "transmit_mhz", "mode", "receive_tone", "transmit_tone", "tone_squelch", "power", "bandwidth", "skip", "tags", "notes", "service", "rx_only", "short_names", "latitude", "longitude", "radius_km"}); err != nil {
		return fmt.Errorf("write CSV header: %w", err)
	}
	for _, c := range channels {
		lat, lon, radius := "", "", ""
		if l := c.Location; l != nil {
			lat = strconv.FormatFloat(l.Latitude, 'f', -1, 64)
			lon = strconv.FormatFloat(l.Longitude, 'f', -1, 64)
			if l.RadiusKM != nil {
				radius = strconv.FormatFloat(*l.RadiusKM, 'f', -1, 64)
			}
		}
		tx := ""
		if c.TransmitHz != nil {
			tx = domain.FormatFrequencyMHz(*c.TransmitHz)
		}
		if err := csvw.Write([]string{c.ID, c.Name, domain.FormatFrequencyMHz(c.ReceiveHz), tx, c.Mode, domain.FormatTone(c.ReceiveTone), domain.FormatTone(c.TransmitTone), strconv.FormatBool(c.ToneSquelch), string(c.Power), c.Bandwidth, strconv.FormatBool(c.Skip), strings.Join(c.Tags, ";"), c.Notes, string(c.Service), strconv.FormatBool(c.RXOnly), strings.Join(c.ShortNames, ";"), lat, lon, radius}); err != nil {
			return fmt.Errorf("write CSV row: %w", err)
		}
	}
	csvw.Flush()
	return csvw.Error()
}
