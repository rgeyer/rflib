// Package importer turns external files into library records and applies
// them to a store. Parsing is pure; Apply does all writes in one transaction.
package importer

import (
	"fmt"
	"strings"

	"github.com/ryan/rflib/internal/domain"
)

// Record is one channel read from a source. When Channel.Group is non-nil it
// identifies the group to attach by ID, then by name; Channel.GroupID is
// ignored.
type Record struct {
	Row     int
	Channel domain.Channel
}

// RowError reports a source row that could not be turned into a Record.
type RowError struct {
	Row int
	Err error
}

func (e RowError) Error() string { return fmt.Sprintf("row %d: %v", e.Row, e.Err) }
func (e RowError) Unwrap() error { return e.Err }

// Batch is everything parsed from one source. Groups holds groups the source
// declares on their own (JSON backups do, CSV does not), so empty groups
// survive a restore.
type Batch struct {
	Groups  []domain.Group
	Records []Record
	Errors  []RowError
}

func (b *Batch) fail(row int, format string, args ...any) {
	b.Errors = append(b.Errors, RowError{Row: row, Err: fmt.Errorf(format, args...)})
}

// finish normalizes and validates a parsed channel, recording it as either a
// Record or a RowError.
func (b *Batch) finish(row int, c domain.Channel) {
	c.Mode = strings.ToUpper(strings.TrimSpace(c.Mode))
	if c.Mode == "" {
		c.Mode = "FM"
	}
	if err := domain.ValidateChannel(c); err != nil {
		b.Errors = append(b.Errors, RowError{Row: row, Err: err})
		return
	}
	b.Records = append(b.Records, Record{Row: row, Channel: c})
}

// Add validates c and records it as a Record or a RowError. Parsers outside
// this package use it so every source normalizes channels the same way.
func (b *Batch) Add(row int, c domain.Channel) { b.finish(row, c) }

// Fail records a row that could not be parsed.
func (b *Batch) Fail(row int, err error) {
	b.Errors = append(b.Errors, RowError{Row: row, Err: err})
}
