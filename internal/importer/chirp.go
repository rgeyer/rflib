package importer

import (
	"io"

	"github.com/ryan/rflib/internal/chirp"
)

// ParseCHIRP reads a CHIRP generic CSV export. Memory locations are not kept;
// rows without a frequency (empty memories) are ignored.
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
		b.finish(row, c)
	}, &b)
	return b, err
}
