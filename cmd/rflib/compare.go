package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/ryan/rflib/internal/app"
	"github.com/ryan/rflib/internal/domain"
	"github.com/ryan/rflib/internal/program"
)

func newRadioCompareCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "compare RADIO FILE", Short: "Compare a radio with a file of what it holds, and bring rflib in line",
		Args: cobra.ExactArgs(2),
		Long: `Compare a radio with FILE, what the radio holds as its programming software or
CHIRP saved it (- reads stdin), memory by memory. Starting a radio from its
current programming is the case where the radio is still empty in rflib.

Each memory is one of:
  same      programmed as rflib would program it
  changed   rflib has it at that memory, but the radio's differs
  radio     only on the radio; rflib lists channels it might be
  rflib     only in rflib; the next export programs it

Memories pair by what they program (frequencies, tones, and mode); then as
changed when only the receive tone or tone squelch differs, such as a
repeater's tone set to transmit only; then by memory number when the receive
frequency or name is the same. A memory rflib's export leaves out, such as tones
the radio's format can't hold, is still compared, with the reason.

--ignore-numbers is for a first import into a radio rflib already lays out its
own way: it checks only that every channel on the radio is in rflib somewhere.
Memories pair by what they program wherever they are, or as changed by name and
receive frequency together; used and kept memories go at the end of the list,
and rflib's order wins from then on.

Without decisions, compare only reports. Decisions change rflib in one go:
  --use 12=ID     put channel ID on the radio for memory 12 (only on the radio)
  --use-exact     do that with the best exact match for every such memory
  --keep 12,14    keep memories as programmed: each becomes a private channel
  --take 3        give rflib the radio's values for changed memories: a
                  private channel changes; a shared one is copied first; a
                  name or skip becomes the radio's own
--keep and --take also take "all". Used and kept memories keep their numbers
where rflib has nothing there, else go at the end of the radio's list.
Memories left undecided stay as rflib has them: the next export programs the
radio from rflib, so a memory only on the radio is then gone.`}
	cmd.Flags().StringArray("use", nil, "MEMORY=ID: use a library or shared channel for a memory only on the radio (repeatable)")
	cmd.Flags().Bool("use-exact", false, "Use the best-ranked exact match for every memory only on the radio that has one")
	cmd.Flags().String("keep", "", "Memories only on the radio to keep as programmed, as private channels: numbers, comma-separated, or all")
	cmd.Flags().String("take", "", "Changed memories whose radio values rflib takes: numbers, comma-separated, or all")
	cmd.Flags().Bool("ignore-numbers", false, "Compare only what memories hold, wherever they are (a first import); added channels go at the end")
	cmd.Flags().Bool("dry-run", false, "Report the comparison and check the decisions without changing anything")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		file, err := readFileArg(cmd, args[1])
		if err != nil {
			return err
		}
		var opts app.CompareOptions
		opts.IgnoreNumbers, _ = cmd.Flags().GetBool("ignore-numbers")
		return withApp(cmd, func(a *app.Service) error {
			cmpn, err := a.CompareRadio(ctx(cmd), args[0], bytes.NewReader(file), opts)
			if err != nil {
				return err
			}
			if err := printComparison(cmd.OutOrStdout(), cmpn); err != nil {
				return err
			}
			for _, e := range cmpn.Errors {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: unreadable %v\n", e)
			}
			d, decided, err := compareDecisions(cmd, cmpn)
			if err != nil || !decided {
				return err
			}
			status := cmd.ErrOrStderr()
			d.DryRun, _ = cmd.Flags().GetBool("dry-run")
			d.Options = opts
			_, res, err := a.ApplyComparison(ctx(cmd), args[0], bytes.NewReader(file), d)
			if err != nil {
				return err
			}
			if d.DryRun {
				fmt.Fprint(status, "Dry run, nothing changed. Would have: ")
			}
			fmt.Fprintf(status, "used %d, kept %d, and took %d memories\n", res.Used, res.Kept, res.Taken)
			if len(res.AtEnd) > 0 {
				fmt.Fprintf(status, "At the end of the list, as rflib uses their numbers: memories %s\n", strings.Join(res.AtEnd, ", "))
			}
			if len(res.Copied) > 0 {
				fmt.Fprintf(status, "Copied from the shared list to take the radio's values: %s\n", strings.Join(res.Copied, ", "))
			}
			return nil
		})
	}
	return cmd
}

// readFileArg reads a file argument, or stdin for "-".
func readFileArg(cmd *cobra.Command, path string) ([]byte, error) {
	if path == "-" {
		return io.ReadAll(cmd.InOrStdin())
	}
	return os.ReadFile(path)
}

func printComparison(out io.Writer, c app.Comparison) error {
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "MEMORY\tSTATUS\tON THE RADIO\tIN RFLIB\tNOTES")
	for _, d := range c.Memories {
		var notes []string
		if len(d.Fields) > 0 {
			notes = append(notes, "differs in "+strings.Join(d.Fields, ", "))
		}
		if d.Unexported != "" {
			notes = append(notes, "rflib's export leaves it out: "+d.Unexported)
		}
		if d.RflibLocation != nil {
			notes = append(notes, fmt.Sprintf("rflib has it at %d", *d.RflibLocation))
		}
		for _, cand := range d.Candidates {
			notes = append(notes, fmt.Sprintf("maybe %s (%s, %s)", cand.Channel.Name, cand.Match, abbrevID(cand.Channel.ID)))
		}
		if d.Kind == app.MemoryOnRadio && len(d.Candidates) == 0 {
			notes = append(notes, "no library channel matches")
		}
		loc := "-" // rflib doesn't program it, so it has no number
		if d.Location != program.NoNumber {
			loc = strconv.Itoa(d.Location)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", loc, d.Kind, describeMemory(d.Radio), describeMemory(d.Rflib), strings.Join(notes, "; "))
	}
	fmt.Fprintf(tw, "\n%d same, %d changed, %d only on the radio, %d only in rflib\n",
		c.Count(app.MemorySame), c.Count(app.MemoryChanged), c.Count(app.MemoryOnRadio), c.Count(app.MemoryInRflib))
	return tw.Flush()
}

// describeMemory is a memory in brief: name, receive MHz, duplex, and tone.
func describeMemory(c *domain.Channel) string {
	if c == nil {
		return "-"
	}
	parts := []string{c.Name, domain.FormatFrequencyMHz(c.ReceiveHz)}
	switch dup := domain.Duplex(*c); dup {
	case "+", "-":
		off := *c.TransmitHz - c.ReceiveHz
		parts = append(parts, fmt.Sprintf("%+g", float64(off)/1e6))
	case "split":
		parts = append(parts, "tx "+domain.FormatFrequencyMHz(*c.TransmitHz))
	case "off":
		parts = append(parts, "rx only")
	}
	if t := toneSummary(*c); t != "" {
		parts = append(parts, t)
	}
	if c.Mode != "FM" {
		parts = append(parts, c.Mode)
	}
	return strings.Join(parts, " ")
}

// abbrevID shortens an ID as tables show it.
func abbrevID(id string) string {
	if len(id) > shortID {
		return id[:shortID]
	}
	return id
}

// compareDecisions reads compare's decision flags against a comparison;
// decided is false when none were given.
func compareDecisions(cmd *cobra.Command, c app.Comparison) (d app.CompareDecisions, decided bool, err error) {
	d.Use = map[int]string{}
	if exact, _ := cmd.Flags().GetBool("use-exact"); exact {
		decided = true
		for _, m := range c.Memories {
			if m.Kind != app.MemoryOnRadio {
				continue
			}
			if i := slices.IndexFunc(m.Candidates, func(c app.Candidate) bool { return c.Match == "exact" }); i >= 0 {
				d.Use[m.Location] = m.Candidates[i].Channel.ID
			}
		}
	}
	uses, _ := cmd.Flags().GetStringArray("use")
	for _, u := range uses {
		loc, id, ok := strings.Cut(u, "=")
		n, err := strconv.Atoi(strings.TrimSpace(loc))
		if !ok || err != nil || strings.TrimSpace(id) == "" {
			return d, false, fmt.Errorf("--use %q: give MEMORY=ID, such as 12=3f2a", u)
		}
		d.Use[n], decided = strings.TrimSpace(id), true
	}
	pick := func(flag string, kind app.MemoryKind) ([]int, error) {
		v := strings.TrimSpace(getString(cmd, flag))
		if v == "" {
			return nil, nil
		}
		decided = true
		var out []int
		if v == "all" {
			for _, m := range c.Memories {
				// "all" kept leaves out memories --use already decided.
				if _, used := d.Use[m.Location]; m.Kind == kind && (kind != app.MemoryOnRadio || !used) {
					out = append(out, m.Location)
				}
			}
			return out, nil
		}
		for _, s := range strings.Split(v, ",") {
			n, err := strconv.Atoi(strings.TrimSpace(s))
			if err != nil {
				return nil, fmt.Errorf("--%s: %q is not a memory number", flag, s)
			}
			if !slices.Contains(out, n) {
				out = append(out, n)
			}
		}
		return out, nil
	}
	if d.Keep, err = pick("keep", app.MemoryOnRadio); err != nil {
		return d, false, err
	}
	if d.Take, err = pick("take", app.MemoryChanged); err != nil {
		return d, false, err
	}
	return d, decided, nil
}
