package main

import (
	"bufio"
	"fmt"
	"strings"

	"github.com/ryan/rflib/internal/app"
	"github.com/ryan/rflib/internal/query"
	"github.com/spf13/cobra"
)

const stdinIDHelp = `IDs may be shortened to any unique prefix of at least 4 characters, as
terminal tables show them. --id - reads IDs from stdin, one per line, taking
the first column, so a list pipes straight in:

  rflib channel list band:2m | rflib radio channels add HT --id -`

// idsFlag adds a repeatable --id that accepts - for stdin.
func idsFlag(cmd *cobra.Command, usage string) {
	cmd.Flags().StringSlice("id", nil, usage+" (repeat or comma-separate; - reads them from stdin)")
}

// getIDs returns --id's values, reading stdin in place of a "-". Blank
// lines, and a header line starting "id", are skipped.
func getIDs(cmd *cobra.Command) ([]string, error) {
	values, _ := cmd.Flags().GetStringSlice("id")
	var out []string
	for _, v := range values {
		if strings.TrimSpace(v) != "-" {
			out = append(out, strings.TrimSpace(v))
			continue
		}
		sc := bufio.NewScanner(cmd.InOrStdin())
		for sc.Scan() {
			first := strings.FieldsFunc(sc.Text(), func(r rune) bool { return r == '\t' || r == ' ' || r == ',' })
			if len(first) == 0 || strings.EqualFold(first[0], "id") {
				continue
			}
			out = append(out, first[0])
		}
		if err := sc.Err(); err != nil {
			return nil, fmt.Errorf("read IDs from stdin: %w", err)
		}
		if len(out) == 0 {
			return nil, fmt.Errorf("no IDs on stdin")
		}
	}
	return out, nil
}

// getID returns a single --id, which may be - for one ID on stdin.
func getID(cmd *cobra.Command) (string, error) {
	v := getString(cmd, "id")
	if strings.TrimSpace(v) != "-" {
		return v, nil
	}
	sc := bufio.NewScanner(cmd.InOrStdin())
	var ids []string
	for sc.Scan() {
		if first := strings.Fields(sc.Text()); len(first) > 0 && !strings.EqualFold(first[0], "id") {
			ids = append(ids, first[0])
		}
	}
	if len(ids) != 1 {
		return "", fmt.Errorf("--id - needs exactly one ID on stdin, got %d", len(ids))
	}
	return ids[0], nil
}

// selection reads which channels a command acts on: --id, --query (and
// any shorthand flags the command has), and --all if it has one.
func selection(cmd *cobra.Command) (app.Selection, error) {
	var sel app.Selection
	var err error
	if sel.IDs, err = getIDs(cmd); err != nil {
		return sel, err
	}
	if cmd.Flags().Lookup("all") != nil {
		sel.All, _ = cmd.Flags().GetBool("all")
	}
	var n query.Node
	if cmd.Flags().Lookup("tag") != nil {
		if filtered(cmd) {
			n, err = channelQuery(cmd, nil)
		}
	} else if text := getString(cmd, "query"); strings.TrimSpace(text) != "" {
		n, err = query.Parse(text)
	}
	if err != nil {
		return sel, err
	}
	sel.Query = n
	if sel.All && (len(sel.IDs) > 0 || sel.Query != nil) {
		return sel, fmt.Errorf("--all can't be combined with --id or --query")
	}
	if !sel.All && len(sel.IDs) == 0 && sel.Query == nil {
		return sel, fmt.Errorf("give channels with --id or --query")
	}
	return sel, nil
}
