package api

import (
	"net/http"

	"github.com/ryan/rflib/internal/app"
	"github.com/ryan/rflib/internal/domain"
)

func (s *server) copyRadio(w http.ResponseWriter, r *http.Request, a *app.Service) error {
	var in radioInput
	if err := readJSON(r, &in); err != nil {
		return err
	}
	if in.Name == nil || in.Model == nil {
		return badRequest("a copy needs a name and a model")
	}
	var to domain.Radio
	if err := in.apply(&to); err != nil {
		return err
	}
	res, err := a.CopyRadio(ctx(r), r.PathValue("radio"), to)
	if err != nil {
		return err
	}
	out, err := s.radioOut(r, a, res.Radio)
	if err != nil {
		return err
	}
	var layoutErr *string
	if res.LayoutErr != nil {
		layoutErr = optional(res.LayoutErr.Error())
	}
	writeJSON(w, http.StatusCreated, map[string]any{"radio": out, "copied": res.Copied, "skipped": newChannels(res.Skipped), "layout_error": layoutErr})
	return nil
}

// MissingChannel is a channel the other radio has and this one doesn't.
type MissingChannel struct {
	Channel  Channel `json:"channel"`
	Section  string  `json:"section"`
	Name     string  `json:"name"`
	Unusable *string `json:"unusable"`
}

func newRadioComparison(c app.RadioComparison) map[string]any {
	missing := make([]MissingChannel, len(c.Missing))
	for i, m := range c.Missing {
		missing[i] = MissingChannel{newChannel(m.Entry.Channel), m.Entry.Section.Name, m.Entry.Name, optional(m.Unusable)}
	}
	return map[string]any{"other": c.Other.Name, "same": c.Same, "missing": missing, "extra": newChannels(c.Extra)}
}

func (s *server) compareRadios(w http.ResponseWriter, r *http.Request, a *app.Service) error {
	var in struct {
		Radio string `json:"radio"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	c, err := a.CompareRadios(ctx(r), r.PathValue("radio"), in.Radio)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, newRadioComparison(c))
	return nil
}

func (s *server) addFromRadio(w http.ResponseWriter, r *http.Request, a *app.Service) error {
	var in struct {
		Radio  string   `json:"radio"`
		Add    []string `json:"add"`
		DryRun bool     `json:"dry_run"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	r, err := editOrViewSaying(w, r, a, in.DryRun)
	if err != nil {
		return err
	}
	_, res, err := a.AddFromRadio(ctx(r), r.PathValue("radio"), in.Radio, in.Add, in.DryRun)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"added": newChannels(res.Added), "new_sections": orEmpty(res.NewSections),
		"unstarted": orEmpty(res.Unstarted), "overflowed": orEmpty(res.Overflowed), "dry_run": in.DryRun})
	return nil
}
