// Package importer turns external files into library records and applies
// them to a store. Parsing is pure; Apply does all writes in one transaction.
package importer

import (
	"fmt"
	"strings"

	"github.com/ryan/rflib/internal/domain"
	"github.com/ryan/rflib/internal/export"
)

// Record is one channel read from a source.
type Record struct {
	Row     int
	Channel domain.Channel
	// Location is the memory number a radio's file gave the channel, when
	// HasLocation (some radios number from 0).
	Location    int
	HasLocation bool
}

// RowError reports a source row that could not be turned into a Record.
type RowError struct {
	Row int
	Err error
}

func (e RowError) Error() string { return fmt.Sprintf("row %d: %v", e.Row, e.Err) }
func (e RowError) Unwrap() error { return e.Err }

// Batch is everything parsed from one source.
type Batch struct {
	Records []Record
	Errors  []RowError
	// Shared is a shared library backup's organizations, systems, and
	// plans; only the shared library restores them.
	Shared export.Records
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

// AddAt is Add for a channel at a memory number.
func (b *Batch) AddAt(row, location int, c domain.Channel) {
	n := len(b.Records)
	b.finish(row, c)
	if len(b.Records) > n {
		b.Records[n].Location, b.Records[n].HasLocation = location, true
	}
}

// ungroup gives a channel what its group meant before groups became
// locations and tags (see sqlite migration 9): the group's name as a tag,
// and its location where the channel has none. Files written before then
// still carry groups.
func ungroup(c *domain.Channel, name string, lat, lon, radiusKM *float64) error {
	if strings.TrimSpace(name) != "" {
		c.Tags = append(c.Tags, name)
	}
	if c.Location != nil || lat == nil {
		return nil
	}
	loc, err := domain.NewLocation(lat, lon, radiusKM)
	c.Location = loc
	return err
}

// Fail records a row that could not be parsed.
func (b *Batch) Fail(row int, err error) {
	b.Errors = append(b.Errors, RowError{Row: row, Err: err})
}
