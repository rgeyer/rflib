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
	if err := csvw.Write([]string{"id", "name", "group_id", "group_name", "group_latitude", "group_longitude", "receive_mhz", "transmit_mhz", "mode", "receive_tone", "transmit_tone", "tone_squelch", "power", "bandwidth", "skip", "tags", "notes", "service", "rx_only"}); err != nil {
		return fmt.Errorf("write CSV header: %w", err)
	}
	for _, c := range channels {
		groupID, groupName, lat, lon := "", "", "", ""
		if c.Group != nil {
			groupID = c.Group.ID
			groupName = c.Group.Name
			if c.Group.Latitude != nil {
				lat = strconv.FormatFloat(*c.Group.Latitude, 'f', 6, 64)
				lon = strconv.FormatFloat(*c.Group.Longitude, 'f', 6, 64)
			}
		}
		tx := ""
		if c.TransmitHz != nil {
			tx = domain.FormatFrequencyMHz(*c.TransmitHz)
		}
		if err := csvw.Write([]string{c.ID, c.Name, groupID, groupName, lat, lon, domain.FormatFrequencyMHz(c.ReceiveHz), tx, c.Mode, domain.FormatTone(c.ReceiveTone), domain.FormatTone(c.TransmitTone), strconv.FormatBool(c.ToneSquelch), c.Power, c.Bandwidth, strconv.FormatBool(c.Skip), strings.Join(c.Tags, ";"), c.Notes, string(c.Service), strconv.FormatBool(c.RXOnly)}); err != nil {
			return fmt.Errorf("write CSV row: %w", err)
		}
	}
	csvw.Flush()
	return csvw.Error()
}
