package api

import (
	"net/http"
	"time"

	"github.com/ryan/rflib/internal/app"
)

// draftHeader names the draft a change was made against: a draft's ID,
// "none" for the saved radio, or "any". A request with it may open a draft
// (the web's first change); one naming a draft or "none" is refused, as
// stale_draft, when the draft open isn't that one. Without it, a change
// needs a draft open.
const draftHeader = "X-Rflib-Draft"

// withDraft adds what the request says about drafts to its context.
func withDraft(r *http.Request) *http.Request {
	v := r.Header.Get(draftHeader)
	e := app.DraftExpectation{Auto: true}
	switch v {
	case "":
		return r
	case "any":
	case "none":
		e.Seen = new(string)
	default:
		e.Seen = &v
	}
	return r.WithContext(app.WithDraftExpectation(r.Context(), e))
}

// edit is h for a change to the list of the radio named by {radio}: it
// goes to the radio's draft (see draftHeader).
func (s *server) edit(h libHandler) libHandler {
	return func(w http.ResponseWriter, r *http.Request, a *app.Service) error {
		r = withDraft(r)
		if err := editRadio(r, a); err != nil {
			return err
		}
		sayDraft(w, r)
		return h(w, r, a)
	}
}

// view is h for a read of the list of the radio named by {radio}: its
// draft's while one is open.
func (s *server) view(h libHandler) libHandler {
	return func(w http.ResponseWriter, r *http.Request, a *app.Service) error {
		if err := viewRadio(r, a); err != nil {
			return err
		}
		return h(w, r, a)
	}
}

// editRadio points {radio} at the radio's draft, for a change.
func editRadio(r *http.Request, a *app.Service) error {
	id, err := a.EditDraft(ctx(r), r.PathValue("radio"))
	if err == nil {
		r.SetPathValue("radio", id)
	}
	return err
}

// sayDraft names, in the response, the draft a change went to, so the
// page's next change is made against it.
func sayDraft(w http.ResponseWriter, r *http.Request) {
	w.Header().Set(draftHeader, r.PathValue("radio"))
}

// viewRadio points {radio} at the radio's open draft, if any, for a read.
func viewRadio(r *http.Request, a *app.Service) error {
	id, _, err := a.ViewDraft(ctx(r), r.PathValue("radio"))
	if err == nil {
		r.SetPathValue("radio", id)
	}
	return err
}

// editOrView is editRadio for a change, and viewRadio for a dry run.
func editOrView(r *http.Request, a *app.Service, dryRun bool) (*http.Request, error) {
	if dryRun {
		return r, viewRadio(r, a)
	}
	r = withDraft(r)
	return r, editRadio(r, a)
}

// editOrViewSaying is editOrView, naming the draft a change went to.
func editOrViewSaying(w http.ResponseWriter, r *http.Request, a *app.Service, dryRun bool) (*http.Request, error) {
	r, err := editOrView(r, a, dryRun)
	if err == nil && !dryRun {
		sayDraft(w, r)
	}
	return r, err
}

// Draft is a radio's open draft.
type Draft struct {
	ID        string    `json:"id"`
	StartedAt time.Time `json:"started_at"`
}

func (s *server) getDraft(w http.ResponseWriter, r *http.Request, a *app.Service) error {
	_, d, ch, err := a.CompareDraft(ctx(r), r.PathValue("radio"))
	if err != nil {
		return err
	}
	var draft *Draft
	if d != nil {
		draft = &Draft{d.ID, d.CreatedAt}
	}
	writeJSON(w, http.StatusOK, map[string]any{"draft": draft, "changes": draftChangesOut(ch)})
	return nil
}

// draftChangesOut is what one list changes from another.
func draftChangesOut(ch app.DraftChanges) map[string]any {
	moves := func(ms []app.MovedMemory) []map[string]any {
		out := make([]map[string]any, len(ms))
		for i, m := range ms {
			out[i] = map[string]any{"channel": newChannel(m.Channel), "from": number(m.From), "to": number(m.To), "from_section": m.FromSec, "to_section": m.ToSec}
		}
		return out
	}
	return map[string]any{
		"added": newChannels(ch.Added), "removed": newChannels(ch.Removed), "changed": channelChanges(ch.Changed),
		"moved": moves(ch.Moved), "renumbered": moves(ch.Renumbered),
		"sections_added": orEmpty(ch.SectionsAdded), "sections_removed": orEmpty(ch.SectionsRemoved),
		"sources_added": orEmpty(ch.SourcesAdded), "sources_removed": orEmpty(ch.SourcesRemoved),
	}
}

// channelChanges are channels, each with what changed about it.
func channelChanges(cs []app.ChannelChange) []map[string]any {
	out := make([]map[string]any, len(cs))
	for i, c := range cs {
		out[i] = map[string]any{"channel": newChannel(c.Channel), "changes": nonNil(c.Changes)}
	}
	return out
}

func (s *server) startDraft(w http.ResponseWriter, r *http.Request, a *app.Service) error {
	d, err := a.StartDraft(ctx(r), r.PathValue("radio"))
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, Draft{d.ID, d.CreatedAt})
	return nil
}

func (s *server) saveDraft(w http.ResponseWriter, r *http.Request, a *app.Service) error {
	radio, err := a.SaveDraft(withDraft(r).Context(), r.PathValue("radio"))
	if err != nil {
		return err
	}
	out, err := s.radioOut(r, a, radio)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

func (s *server) discardDraft(w http.ResponseWriter, r *http.Request, a *app.Service) error {
	radio, err := a.DiscardDraft(withDraft(r).Context(), r.PathValue("radio"))
	if err != nil {
		return err
	}
	out, err := s.radioOut(r, a, radio)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}
