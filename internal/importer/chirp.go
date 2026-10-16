package importer

import (
	"io"
	"strings"

	"github.com/ryan/rflib/internal/chirp"
)

// ParseCHIRP reads a CHIRP generic CSV export, keeping each record's memory
// location; rows without a frequency (empty memories) are ignored.
func ParseCHIRP(r io.Reader) (Batch, error) {
	var b Batch
	err := readCSV(r, []string{"frequency"}, func(row int, get func(string) string) {
		if get("frequency") == "" {
			return
		}
		m, err := chirp.ParseRecord(func(col string) string { return get(lower(col)) })
		if err != nil {
			b.fail(row, "%v", err)
			return
		}
		c, err := chirp.ToChannel(m)
		if err != nil {
			b.fail(row, "%v", err)
			return
		}
		if strings.TrimSpace(get("location")) == "" {
			b.finish(row, c)
			return
		}
		b.AddAt(row, m.Location, c)
	}, &b)
	return b, err
}
