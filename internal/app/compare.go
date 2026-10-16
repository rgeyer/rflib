package app

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/ryan/rflib/internal/domain"
	"github.com/ryan/rflib/internal/importer"
	"github.com/ryan/rflib/internal/program"
	"github.com/ryan/rflib/internal/store"
)

// Comparing a radio with its own file: the file is what the radio holds
// (saved by its programming software or CHIRP), and rflib's side is the
// radio's export, read back with the same parser, so rounding, tone
// formats, and fitted names compare like for like. Memories pair by what
// they program (frequencies, tones, and mode), preferring the same memory
// number, and then by memory number when the receive frequency or the name
// is the same. Starting a radio from its file is the
// case where rflib's side is empty.
//
// Ignoring memory numbers is for a first import into a radio rflib already
// lays out its own way: it checks that every channel on the radio is in
// rflib somewhere, and what's missing joins the end of rflib's list, whose
// order wins from then on.

// MemoryKind is how a memory compares.
type MemoryKind string

const (
	// MemorySame is programmed as rflib would program it.
	MemorySame MemoryKind = "same"
	// MemoryChanged differs from what rflib would program there.
	MemoryChanged MemoryKind = "changed"
	// MemoryOnRadio is on the radio only.
	MemoryOnRadio MemoryKind = "radio"
	// MemoryInRflib is in rflib only; the next export programs it.
	MemoryInRflib MemoryKind = "rflib"
)

// MemoryDiff is one memory of a comparison.
type MemoryDiff struct {
	Kind MemoryKind
	// Location is the memory number on the radio, or for MemoryInRflib
	// where rflib puts it.
	Location int
	// RflibLocation is where rflib puts the channel, when that differs
	// from Location.
	RflibLocation *int
	// Radio is the memory as the file has it, and Rflib as rflib's export
	// has it.
	Radio, Rflib *domain.Channel
	// Channel is the library channel behind rflib's side.
	Channel *domain.Channel
	// Fields are what differs, for MemoryChanged.
	Fields []string
	// Candidates are channels a MemoryOnRadio might be, best first.
	Candidates []Candidate
	// Unexported says why rflib's export leaves this memory out, such as
	// tones the radio's format can't hold; rflib's side is then the
	// channel as rflib has it.
	Unexported string
}

// Candidate is a channel a memory might be, and how well it matches:
// "exact" (frequencies, tones, and mode), "frequencies" (tones or mode
// differ), or "receive" (only the receive frequency).
type Candidate struct {
	Channel domain.Channel
	Match   string
}

// Comparison is a radio against its file, by memory number.
type Comparison struct {
	Radio    domain.Radio
	Memories []MemoryDiff
	// Errors are file rows that couldn't be read.
	Errors []importer.RowError
}

// Count returns how many memories are of a kind.
func (c Comparison) Count(k MemoryKind) int {
	n := 0
	for _, d := range c.Memories {
		if d.Kind == k {
			n++
		}
	}
	return n
}

// find returns the memory at a radio memory number, of the kind given.
func (c Comparison) find(loc int, k MemoryKind) (MemoryDiff, error) {
	for _, d := range c.Memories {
		if d.Location == loc && d.Kind == k {
			return d, nil
		}
	}
	what := map[MemoryKind]string{MemoryOnRadio: "only on the radio", MemoryChanged: "changed on the radio"}[k]
	return MemoryDiff{}, domain.Invalidf("memory %d isn't %s; see rflib radio compare %s FILE", loc, what, c.Radio.Name)
}

const maxCandidates = 5

// CompareOptions are how to compare.
type CompareOptions struct {
	// IgnoreNumbers compares only what memories hold, wherever they are:
	// memories pair by what they program, or as changed by name and
	// receive frequency together, never by memory number.
	IgnoreNumbers bool
}

// CompareRadio compares a radio with a file of what it holds.
func (s *Service) CompareRadio(ctx context.Context, radio string, file io.Reader, opts CompareOptions) (Comparison, error) {
	r, m, err := s.RadioModel(ctx, radio)
	if err != nil {
		return Comparison{}, err
	}
	cmpn := Comparison{Radio: r}
	got, err := program.Parse(m, file)
	if err != nil {
		return cmpn, err
	}
	cmpn.Errors = got.Errors
	for _, rec := range got.Records {
		if !rec.HasLocation {
			return cmpn, domain.Invalidf("row %d has no memory number; compare needs the file the radio's software or CHIRP saved", rec.Row)
		}
	}
	layout, entries, err := s.Layout(ctx, r.ID)
	if err != nil {
		return cmpn, err
	}
	var buf bytes.Buffer
	if _, err := program.Write(&buf, layout, true, io.Discard); err != nil {
		return cmpn, err
	}
	want, err := program.Parse(m, &buf)
	if err != nil {
		return cmpn, fmt.Errorf("read back %s's export: %w", r.Name, err)
	}
	// rflib's side, each with its library channel: the export read back,
	// by memory number, then the memories the programming leaves out, as
	// rflib has them (they have no number), saying why.
	byLocation := map[int]program.Memory{}
	for _, mem := range layout.Memories {
		if mem.Programmed() {
			byLocation[mem.Location] = mem
		}
	}
	rflibSide := want.Records
	original := make([]domain.Channel, len(rflibSide))
	unexported := make([]string, len(rflibSide))
	for j, rec := range rflibSide {
		original[j] = byLocation[rec.Location].Original
	}
	for _, mem := range layout.Memories {
		if !mem.Programmed() {
			rflibSide = append(rflibSide, importer.Record{Channel: mem.Channel, Location: program.NoNumber})
			original = append(original, mem.Original)
			unexported = append(unexported, mem.NotProgrammed())
		}
	}

	radioSide := got.Records
	pairedRadio := make([]bool, len(radioSide))
	pairedRflib := make([]bool, len(rflibSide))
	pair := func(i, j int, kind MemoryKind) {
		pairedRadio[i], pairedRflib[j] = true, true
		a, b, ch := radioSide[i].Channel, rflibSide[j].Channel, original[j]
		d := MemoryDiff{Kind: kind, Location: radioSide[i].Location, Radio: &a, Rflib: &b, Channel: &ch, Fields: diffFields(a, b)}
		if l := rflibSide[j].Location; l != d.Location && l != program.NoNumber {
			d.RflibLocation = &l
		}
		d.Unexported = unexported[j]
		if d.Kind == MemorySame && len(d.Fields) > 0 {
			d.Kind = MemoryChanged
		}
		cmpn.Memories = append(cmpn.Memories, d)
	}
	// First by what they program: the same memory number first (unless
	// numbers are ignored), then the same name, then the nearest number.
	closer := func(a importer.Record, b, best importer.Record) bool {
		rank := func(x importer.Record) [3]int {
			r := [3]int{0, 0, abs(x.Location - a.Location)}
			if !opts.IgnoreNumbers && x.Location != a.Location {
				r[0] = 1
			}
			if !strings.EqualFold(x.Channel.Name, a.Channel.Name) {
				r[1] = 1
			}
			return r
		}
		rb, rbest := rank(b), rank(best)
		return slices.Compare(rb[:], rbest[:]) < 0
	}
	for i, a := range radioSide {
		best := -1
		for j, b := range rflibSide {
			if pairedRflib[j] || airKey(a.Channel) != airKey(b.Channel) {
				continue
			}
			if best < 0 || closer(a, b, rflibSide[best]) {
				best = j
			}
		}
		if best >= 0 {
			pair(i, best, MemorySame)
		}
	}
	// Then as changed when only how the radio listens differs: the same
	// frequencies, mode, and transmit tone, but another receive tone or tone
	// squelch. A transmit tone that opens a repeater with no receive tone
	// set is common, and works.
	for i, a := range radioSide {
		if pairedRadio[i] {
			continue
		}
		best := -1
		for j, b := range rflibSide {
			if pairedRflib[j] || listenKey(a.Channel) != listenKey(b.Channel) {
				continue
			}
			if best < 0 || closer(a, b, rflibSide[best]) {
				best = j
			}
		}
		if best >= 0 {
			pair(i, best, MemoryChanged)
		}
	}
	// Then as changed, when they're still recognizably the same channel: at
	// the same memory number with the same receive frequency or name, or,
	// with numbers ignored, anywhere with the same name and receive
	// frequency. Anything else is a different channel.
	for i, a := range radioSide {
		if pairedRadio[i] {
			continue
		}
		for j, b := range rflibSide {
			sameRX, sameName := a.Channel.ReceiveHz == b.Channel.ReceiveHz, strings.EqualFold(a.Channel.Name, b.Channel.Name)
			match := b.Location == a.Location && (sameRX || sameName)
			if opts.IgnoreNumbers {
				match = sameRX && sameName
			}
			if !pairedRflib[j] && match {
				pair(i, j, MemoryChanged)
				break
			}
		}
	}
	var pool []domain.Channel
	if slices.Contains(pairedRadio, false) {
		if pool, err = s.candidatePool(ctx, entries); err != nil {
			return cmpn, err
		}
	}
	for i, a := range radioSide {
		if !pairedRadio[i] {
			c := a.Channel
			cmpn.Memories = append(cmpn.Memories, MemoryDiff{Kind: MemoryOnRadio, Location: a.Location, Radio: &c, Candidates: candidates(c, pool)})
		}
	}
	for j, b := range rflibSide {
		if !pairedRflib[j] {
			c, ch := b.Channel, original[j]
			cmpn.Memories = append(cmpn.Memories, MemoryDiff{Kind: MemoryInRflib, Location: b.Location, Rflib: &c, Channel: &ch, Unexported: unexported[j]})
		}
	}
	slices.SortStableFunc(cmpn.Memories, func(a, b MemoryDiff) int { return cmp.Compare(a.Location, b.Location) })
	return cmpn, nil
}

// candidatePool is what a memory only on the radio might be: your channels
// and the shared list, less what's on the radio already.
func (s *Service) candidatePool(ctx context.Context, entries []domain.RadioEntry) ([]domain.Channel, error) {
	all, err := s.scoped(ctx, "all", true)
	if err != nil {
		return nil, err
	}
	on := map[string]bool{}
	for _, e := range entries {
		on[e.Channel.ID] = true
	}
	return slices.DeleteFunc(all, func(c domain.Channel) bool { return on[c.ID] }), nil
}

// candidates ranks the channels a memory might be: by how much of the
// programming matches and how close the name is, together, so a channel of
// the same name with other tones can beat an exact match of another name;
// then by the programming alone, then your own before shared ones.
func candidates(mem domain.Channel, pool []domain.Channel) []Candidate {
	var out []Candidate
	for _, c := range pool {
		if c.ReceiveHz != mem.ReceiveHz {
			continue
		}
		match := "receive"
		switch {
		case airKey(c) == airKey(mem):
			match = "exact"
		case equalTX(onAir(c), onAir(mem)):
			match = "frequencies"
		}
		out = append(out, Candidate{Channel: c, Match: match})
	}
	rank := map[string]int{"exact": 0, "frequencies": 1, "receive": 2}
	slices.SortStableFunc(out, func(a, b Candidate) int {
		return cmp.Or(
			cmp.Compare(rank[a.Match]+nameDistance(mem.Name, a.Channel), rank[b.Match]+nameDistance(mem.Name, b.Channel)),
			cmp.Compare(rank[a.Match], rank[b.Match]),
			cmp.Compare(boolInt(a.Channel.Link != nil), boolInt(b.Channel.Link != nil)),
			strings.Compare(a.Channel.Name, b.Channel.Name))
	})
	if len(out) > maxCandidates {
		out = out[:maxCandidates]
	}
	return out
}

// nameDistance is 0 when a radio's name is the channel's name or one of its
// short names, 1 when one contains it, and 2 otherwise.
func nameDistance(name string, c domain.Channel) int {
	name = strings.ToLower(strings.TrimSpace(name))
	best := 2
	for _, n := range append([]string{c.Name}, c.ShortNames...) {
		n = strings.ToLower(n)
		switch {
		case n == name:
			return 0
		case name != "" && strings.Contains(n, name):
			best = 1
		}
	}
	return best
}

// onAir is a channel as a radio transmits it: a receive-only channel has
// no transmit frequency.
func onAir(c domain.Channel) domain.Channel {
	if c.RXOnly {
		c.TransmitHz = nil
	}
	return c
}

// airKey is what a memory programs: frequencies, tones, and mode.
func airKey(c domain.Channel) string {
	return domain.ProgrammedKey(onAir(c)) + "\x00" + c.Mode
}

// listenKey is airKey without how the radio listens: frequencies, mode,
// and the transmit tone.
func listenKey(c domain.Channel) string {
	c = onAir(c)
	return strings.Join([]string{airKey(domain.Channel{ReceiveHz: c.ReceiveHz, TransmitHz: c.TransmitHz, Mode: c.Mode}), domain.FormatTone(c.TransmitTone)}, "\x00")
}

func equalTX(a, b domain.Channel) bool {
	return a.TransmitHz == nil && b.TransmitHz == nil || a.TransmitHz != nil && b.TransmitHz != nil && *a.TransmitHz == *b.TransmitHz
}

// diffFields lists what differs between a memory on the radio and rflib's.
func diffFields(radio, rflib domain.Channel) []string {
	radio, rflib = onAir(radio), onAir(rflib)
	var out []string
	for _, f := range []struct {
		name string
		same bool
	}{
		{"name", radio.Name == rflib.Name},
		{"rx", radio.ReceiveHz == rflib.ReceiveHz},
		{"tx", equalTX(radio, rflib)},
		{"mode", radio.Mode == rflib.Mode},
		{"tx tone", domain.FormatTone(radio.TransmitTone) == domain.FormatTone(rflib.TransmitTone)},
		{"rx tone", domain.FormatTone(radio.ReceiveTone) == domain.FormatTone(rflib.ReceiveTone)},
		{"tone squelch", radio.ToneSquelch == rflib.ToneSquelch},
		{"skip", radio.Skip == rflib.Skip},
	} {
		if !f.same {
			out = append(out, f.name)
		}
	}
	return out
}

// CompareDecisions are what to do with a comparison's memories, by radio
// memory number; memories not named are left as they are in rflib.
type CompareDecisions struct {
	// Use puts a channel (by ID or ID prefix) on the radio for a memory
	// only on the radio, and Keep a private channel made from the memory.
	Use  map[int]string
	Keep []int
	// Take gives rflib the radio's values for a changed memory.
	Take []int
	// DryRun works everything out, then changes nothing.
	DryRun bool
	// Options are how the comparison was made; with IgnoreNumbers, used
	// and kept memories go at the end of the radio's list, in the radio's
	// order.
	Options CompareOptions
}

// CompareResult is what applying decisions did.
type CompareResult struct {
	Used, Kept, Taken int
	// AtEnd are memories whose number is within what the radio already
	// uses, so they went at the end of the default section. With numbers
	// ignored, every used and kept memory goes there, and none are listed.
	AtEnd []string
	// Copied are shared channels copied to take the radio's values.
	Copied []string
}

// ApplyComparison compares a radio with its file and applies decisions, in
// one transaction. Used and kept memories numbered past everything the
// radio uses keep their numbers, in new sections starting at each run of
// numbers (2-3 and 10 make two); the rest go at the end of the default
// section. Taking a changed
// memory's frequencies, tones, or mode changes a private channel, or makes
// a private copy of a shared one; its name and skip become the radio's
// overrides.
func (s *Service) ApplyComparison(ctx context.Context, radio string, file io.Reader, d CompareDecisions) (Comparison, CompareResult, error) {
	var res CompareResult
	if err := s.writable(); err != nil {
		return Comparison{}, res, err
	}
	cmpn, err := s.CompareRadio(ctx, radio, file, d.Options)
	if err != nil {
		return cmpn, res, err
	}
	r, m, err := s.RadioModel(ctx, cmpn.Radio.ID)
	if err != nil {
		return cmpn, res, err
	}
	var places []placement
	for loc, ref := range d.Use {
		mem, err := cmpn.find(loc, MemoryOnRadio)
		if err != nil {
			return cmpn, res, err
		}
		ids, err := s.ChannelIDs(ctx, []string{ref})
		if err != nil {
			return cmpn, res, fmt.Errorf("memory %d: %w", loc, err)
		}
		places = append(places, placement{loc: loc, id: ids[0], name: mem.Radio.Name})
	}
	for _, loc := range d.Keep {
		mem, err := cmpn.find(loc, MemoryOnRadio)
		if err != nil {
			return cmpn, res, err
		}
		if _, used := d.Use[loc]; used {
			return cmpn, res, domain.Invalidf("memory %d: use a channel or keep it as programmed, not both", loc)
		}
		places = append(places, placement{loc: loc, name: mem.Radio.Name, keep: mem.Radio})
	}
	var takes []MemoryDiff
	for _, loc := range d.Take {
		mem, err := cmpn.find(loc, MemoryChanged)
		if err != nil {
			return cmpn, res, err
		}
		takes = append(takes, mem)
	}
	slices.SortFunc(places, func(a, b placement) int { return cmp.Compare(a.loc, b.loc) })
	err = s.lib.InTx(ctx, func(tx store.Library) error {
		for _, mem := range takes {
			copied, err := takeRadio(ctx, tx, r, mem)
			if err != nil {
				return fmt.Errorf("memory %d: %w", mem.Location, err)
			}
			if copied {
				res.Copied = append(res.Copied, mem.Channel.Name)
			}
			res.Taken++
		}
		// Past everything the radio uses, a memory keeps its number.
		last := m.Memories.First - 1
		if !d.Options.IgnoreNumbers {
			if layout, _, err := layoutOf(ctx, tx, r, m); err != nil {
				return err
			} else {
				for _, mem := range layout.Memories {
					last = max(last, mem.Location)
				}
			}
		}
		var runs [][]placement
		var atEnd []placement
		for _, p := range places {
			if p.keep != nil {
				c := *p.keep
				c.ID = ""
				made, err := tx.CreateChannel(ctx, c)
				if err != nil {
					return fmt.Errorf("memory %d: %w", p.loc, err)
				}
				p.id = made.ID
				res.Kept++
			} else {
				res.Used++
			}
			switch n := len(runs); {
			case d.Options.IgnoreNumbers || p.loc <= last || p.loc > m.Memories.Last:
				atEnd = append(atEnd, p)
			case n > 0 && runs[n-1][len(runs[n-1])-1].loc == p.loc-1:
				runs[n-1] = append(runs[n-1], p)
			default:
				runs = append(runs, []placement{p})
			}
		}
		for _, p := range atEnd {
			if _, err := tx.AddRadioChannels(ctx, r.ID, "", []string{p.id}); err != nil {
				return fmt.Errorf("memory %d: %w", p.loc, err)
			}
			if !d.Options.IgnoreNumbers {
				res.AtEnd = append(res.AtEnd, fmt.Sprintf("%d %q", p.loc, p.name))
			}
		}
		if err := placeRuns(ctx, tx, r, runs); err != nil {
			return err
		}
		if _, _, err := layoutOf(ctx, tx, r, m); err != nil {
			return fmt.Errorf("%s would no longer export: %w", r.Name, err)
		}
		if d.DryRun {
			return errDryRun
		}
		return nil
	})
	if errors.Is(err, errDryRun) {
		err = nil
	}
	return cmpn, res, err
}

// takeRadio gives rflib a changed memory's values from the radio. It
// reports whether a shared channel was copied to do so.
func takeRadio(ctx context.Context, tx store.Library, r domain.Radio, mem MemoryDiff) (bool, error) {
	ch, radio := *mem.Channel, onAir(*mem.Radio)
	channelFields := slices.ContainsFunc(mem.Fields, func(f string) bool { return f != "name" && f != "skip" })
	copied := false
	if channelFields {
		if ch.Link != nil {
			cp, err := copyShared(ctx, tx, ch)
			if err != nil {
				return false, err
			}
			ch, copied = cp, true
		}
		ch.ReceiveHz, ch.TransmitHz, ch.Mode = radio.ReceiveHz, radio.TransmitHz, radio.Mode
		ch.ReceiveTone, ch.TransmitTone, ch.ToneSquelch = radio.ReceiveTone, radio.TransmitTone, radio.ToneSquelch
		ch.RXOnly = ch.RXOnly && ch.TransmitHz != nil
		if _, err := tx.UpdateChannel(ctx, ch); err != nil {
			return copied, err
		}
	}
	if !slices.Contains(mem.Fields, "name") && !slices.Contains(mem.Fields, "skip") {
		return copied, nil
	}
	entries, err := tx.RadioEntries(ctx, r.ID)
	if err != nil {
		return copied, err
	}
	for _, e := range entries {
		if e.Channel.ID != ch.ID {
			continue
		}
		if slices.Contains(mem.Fields, "name") {
			e.Name = mem.Radio.Name
		}
		if slices.Contains(mem.Fields, "skip") {
			skip := mem.Radio.Skip
			e.Skip = &skip
		}
		return copied, tx.SetRadioEntry(ctx, r.ID, e)
	}
	return copied, fmt.Errorf("%s is no longer on %s", ch.Name, r.Name)
}

// placement is a memory a comparison puts on the radio: a channel to use
// (id), or the memory to keep as programmed (keep).
type placement struct {
	loc  int
	id   string
	name string
	keep *domain.Channel
}

// placeRuns puts each run of memories, numbered past everything on the
// radio, in a new section starting at its first number, after the
// sections there are (and after the default section, when that holds
// channels, so their numbers stay).
func placeRuns(ctx context.Context, tx store.Library, r domain.Radio, runs [][]placement) error {
	for _, run := range runs {
		first, end := run[0].loc, run[len(run)-1].loc
		name := fmt.Sprintf("From the radio %d", first)
		if end > first {
			name = fmt.Sprintf("From the radio %d-%d", first, end)
		}
		sec, err := tx.CreateRadioSection(ctx, domain.RadioSection{RadioID: r.ID, Name: name, Start: &first})
		if err != nil {
			return err
		}
		secs, err := tx.RadioSections(ctx, r.ID)
		if err != nil {
			return err
		}
		es, err := tx.RadioEntries(ctx, r.ID)
		if err != nil {
			return err
		}
		for _, e := range es {
			if e.Section.Default {
				if err := tx.MoveRadioSection(ctx, r.ID, sec.ID, len(secs)-1); err != nil {
					return err
				}
				break
			}
		}
		ids := make([]string, len(run))
		for i, p := range run {
			ids[i] = p.id
		}
		if _, err := tx.AddRadioChannels(ctx, r.ID, sec.ID, ids); err != nil {
			return err
		}
	}
	return nil
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
