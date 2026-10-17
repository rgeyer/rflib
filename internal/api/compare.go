package api

import (
	"net/http"
	"strings"

	"github.com/ryan/rflib/internal/app"
	"github.com/ryan/rflib/internal/domain"
	"github.com/ryan/rflib/internal/program"
)

// MemoryValues is a memory as programmed: what a comparison shows of each
// side.
type MemoryValues struct {
	Name        string `json:"name"`
	RxHz        int64  `json:"rx_hz"`
	TxHz        *int64 `json:"tx_hz"`
	Mode        string `json:"mode"`
	RxTone      string `json:"rx_tone"`
	TxTone      string `json:"tx_tone"`
	ToneSquelch bool   `json:"tone_squelch"`
	Skip        bool   `json:"skip"`
}

func newMemoryValues(c *domain.Channel) *MemoryValues {
	if c == nil {
		return nil
	}
	tx := c.TransmitHz
	if c.RXOnly {
		tx = nil
	}
	return &MemoryValues{c.Name, c.ReceiveHz, tx, c.Mode, domain.FormatTone(c.ReceiveTone), domain.FormatTone(c.TransmitTone), c.ToneSquelch, c.Skip}
}

// Candidate is a channel a memory only on the radio might be.
type Candidate struct {
	Match   string   `json:"match"`
	Channel Channel  `json:"channel"`
	Differs []string `json:"differs"`
}

// MemoryDiff is one memory of a comparison.
type MemoryDiff struct {
	Memory      *int          `json:"memory"`
	Status      string        `json:"status"`
	RflibMemory *int          `json:"rflib_memory"`
	Radio       *MemoryValues `json:"radio"`
	Rflib       *MemoryValues `json:"rflib"`
	Channel     *Channel      `json:"channel"`
	Fields      []string      `json:"fields"`
	Candidates  []Candidate   `json:"candidates"`
	Unexported  *string       `json:"unexported"`
}

type compareInput struct {
	File          string `json:"file"`
	IgnoreNumbers bool   `json:"ignore_numbers"`
}

func (in compareInput) options() app.CompareOptions {
	return app.CompareOptions{IgnoreNumbers: in.IgnoreNumbers}
}

func newComparison(c app.Comparison) map[string]any {
	out := make([]MemoryDiff, len(c.Memories))
	for i, d := range c.Memories {
		m := MemoryDiff{Memory: number(d.Location), Status: string(d.Kind), Radio: newMemoryValues(d.Radio), Rflib: newMemoryValues(d.Rflib),
			Fields: d.Fields, Candidates: []Candidate{}}
		if m.Fields == nil {
			m.Fields = []string{}
		}
		if d.Unexported != "" {
			why := d.Unexported
			m.Unexported = &why
		}
		m.RflibMemory = d.RflibLocation
		if d.Channel != nil {
			ch := newChannel(*d.Channel)
			m.Channel = &ch
		}
		for _, cand := range d.Candidates {
			m.Candidates = append(m.Candidates, Candidate{cand.Match, newChannel(cand.Channel), nonNil(cand.Differs)})
		}
		out[i] = m
	}
	errs := make([]string, len(c.Errors))
	for i, e := range c.Errors {
		errs[i] = e.Error()
	}
	return map[string]any{"memories": out, "errors": errs}
}

func (s *server) compareRadio(w http.ResponseWriter, r *http.Request, a *app.Service) error {
	var in compareInput
	if err := readJSON(r, &in); err != nil {
		return err
	}
	c, err := a.CompareRadio(ctx(r), r.PathValue("radio"), strings.NewReader(in.File), in.options())
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, newComparison(c))
	return nil
}

func (s *server) applyComparison(w http.ResponseWriter, r *http.Request, a *app.Service) error {
	var in struct {
		compareInput
		Use []struct {
			Memory    int    `json:"memory"`
			ChannelID string `json:"channel_id"`
		} `json:"use"`
		Keep   []int `json:"keep"`
		Take   []int `json:"take"`
		DryRun bool  `json:"dry_run"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	d := app.CompareDecisions{Use: map[int]string{}, Keep: in.Keep, Take: in.Take, DryRun: in.DryRun, Options: in.options()}
	for _, u := range in.Use {
		if _, dup := d.Use[u.Memory]; dup {
			return badRequest("memory %d is used twice", u.Memory)
		}
		d.Use[u.Memory] = u.ChannelID
	}
	_, res, err := a.ApplyComparison(ctx(r), r.PathValue("radio"), strings.NewReader(in.File), d)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"used": res.Used, "kept": res.Kept, "taken": res.Taken,
		"at_end": orEmpty(res.AtEnd), "copied": orEmpty(res.Copied), "dry_run": in.DryRun})
	return nil
}

// number is a memory number, or nil for one that isn't programmed.
func number(loc int) *int {
	if loc == program.NoNumber {
		return nil
	}
	return &loc
}

func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
