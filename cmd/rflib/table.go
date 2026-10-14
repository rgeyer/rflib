package main

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

// field is one column a list command can print. Its name is what --fields
// and JSON use; text shows it in a table, and json, when set, gives its
// JSON value (else the text).
type field[T any] struct {
	name string
	text func(T) string
	json func(T) any
	// id marks an ID column, which terminal tables shorten.
	id bool
}

// listing is what a list command can print: its fields, the ones shown by
// default, and optionally the full JSON document --format json writes
// when --fields isn't given (else one object per row, of every field).
type listing[T any] struct {
	fields   []field[T]
	defaults []string
	full     func([]T) any
}

const tableHelp = `Output: on a terminal, an aligned table with a header and IDs shortened to
8 characters (any unique prefix of 4 or more is accepted back); piped, plain
tab-separated rows without a header and with full IDs, for scripts. --fields
picks and orders the columns, --format tsv|csv|json chooses another format,
and --no-header drops the header row.`

// shortID is how many characters of an ID terminal tables show. Any unique
// prefix of at least app.MinIDPrefix is accepted back.
const shortID = 8

// isTerminal reports whether w is a terminal; tests replace it.
var isTerminal = func(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func (l listing[T]) names() []string {
	out := make([]string, len(l.fields))
	for i, f := range l.fields {
		out[i] = f.name
	}
	return out
}

// flags adds --fields, --format, and --no-header to a list command.
func (l listing[T]) flags(cmd *cobra.Command) {
	cmd.Flags().String("fields", "", fmt.Sprintf("Comma-separated columns to show, in order (default %s; all: %s)",
		strings.Join(l.defaults, ","), strings.Join(l.names(), ",")))
	cmd.Flags().String("format", "text", "Output format: text (aligned on a terminal, tab-separated otherwise), tsv, csv, or json")
	cmd.Flags().Bool("no-header", false, "Leave out the header row in text, tsv, and csv output")
}

// check validates the output flags before any work is done.
func (l listing[T]) check(cmd *cobra.Command) error {
	if _, err := l.selected(cmd); err != nil {
		return err
	}
	switch f := format(cmd); f {
	case "text", "tsv", "csv", "json":
		return nil
	default:
		return fmt.Errorf("unknown format %q; use text, tsv, csv, or json", f)
	}
}

func (l listing[T]) selected(cmd *cobra.Command) ([]field[T], error) {
	names := l.defaults
	if v := strings.TrimSpace(getString(cmd, "fields")); v != "" {
		names = strings.Split(v, ",")
	}
	out := make([]field[T], 0, len(names))
	for _, name := range names {
		name = strings.ToLower(strings.TrimSpace(name))
		i := slices.IndexFunc(l.fields, func(f field[T]) bool { return f.name == name })
		if i < 0 {
			return nil, fmt.Errorf("unknown field %q; use %s", name, strings.Join(l.names(), ", "))
		}
		out = append(out, l.fields[i])
	}
	return out, nil
}

// write prints rows to the command's stdout as the output flags say.
func (l listing[T]) write(cmd *cobra.Command, rows []T) error {
	if err := l.check(cmd); err != nil {
		return err
	}
	fields, _ := l.selected(cmd)
	out := cmd.OutOrStdout()
	header, _ := cmd.Flags().GetBool("no-header")
	header = !header
	switch format(cmd) {
	case "json":
		if l.full != nil && !cmd.Flags().Changed("fields") {
			return writeJSON(out, l.full(rows))
		}
		if !cmd.Flags().Changed("fields") {
			fields = l.fields
		}
		objs := make([]jsonObject, len(rows))
		for i, r := range rows {
			for _, f := range fields {
				var v any
				if f.json != nil {
					v = f.json(r)
				} else {
					v = f.text(r)
				}
				objs[i] = append(objs[i], jsonPair{f.name, v})
			}
		}
		return writeJSON(out, objs)
	case "csv":
		w := csv.NewWriter(out)
		if header {
			_ = w.Write(l.header(fields, false))
		}
		for _, r := range rows {
			_ = w.Write(cells(fields, r, false))
		}
		w.Flush()
		return w.Error()
	case "tsv":
		return writeTabs(out, fields, rows, header, false)
	}
	if !isTerminal(out) {
		// Piped text is for scripts: no header, full IDs.
		return writeTabs(out, fields, rows, false, false)
	}
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	if err := writeTabs(tw, fields, rows, header, true); err != nil {
		return err
	}
	return tw.Flush()
}

// format is --format, or text for a command without one (such as a show
// command printing a table).
func format(cmd *cobra.Command) string {
	return orDefault(getString(cmd, "format"), "text")
}

func (l listing[T]) header(fields []field[T], upper bool) []string {
	out := make([]string, len(fields))
	for i, f := range fields {
		out[i] = f.name
		if upper {
			out[i] = strings.ToUpper(f.name)
		}
	}
	return out
}

func cells[T any](fields []field[T], r T, short bool) []string {
	out := make([]string, len(fields))
	for i, f := range fields {
		out[i] = f.text(r)
		if short && f.id && len(out[i]) > shortID {
			out[i] = out[i][:shortID]
		}
	}
	return out
}

// writeTabs writes tab-separated rows. Tabs and newlines inside values
// become spaces so they can't break the columns.
func writeTabs[T any](w io.Writer, fields []field[T], rows []T, header, terminal bool) error {
	clean := strings.NewReplacer("\t", " ", "\n", " ", "\r", " ")
	line := func(vs []string) error {
		for i := range vs {
			vs[i] = clean.Replace(vs[i])
		}
		_, err := fmt.Fprintln(w, strings.Join(vs, "\t"))
		return err
	}
	if header {
		if err := line(listing[T]{}.header(fields, terminal)); err != nil {
			return err
		}
	}
	for _, r := range rows {
		if err := line(cells(fields, r, terminal)); err != nil {
			return err
		}
	}
	return nil
}

// jsonObject is a JSON object that keeps its keys in order.
type jsonObject []jsonPair

type jsonPair struct {
	key   string
	value any
}

func (o jsonObject) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, p := range o {
		if i > 0 {
			b.WriteByte(',')
		}
		k, err := json.Marshal(p.key)
		if err != nil {
			return nil, err
		}
		v, err := json.Marshal(p.value)
		if err != nil {
			return nil, err
		}
		b.Write(k)
		b.WriteByte(':')
		b.Write(v)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}
