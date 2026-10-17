package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/ryan/rflib/internal/app"
	"github.com/ryan/rflib/internal/domain"
)

// RadioRevision is a radio revision, without its contents.
type RadioRevision struct {
	Number   int       `json:"number"`
	At       time.Time `json:"at"`
	Reason   string    `json:"reason"`
	Author   string    `json:"author"`
	Exported bool      `json:"exported"`
	Memories int       `json:"memories"`
}

func newRadioRevision(r domain.RadioRevision) RadioRevision {
	return RadioRevision{r.Number, r.At, r.Reason, r.Author, r.Exported, len(r.Snapshot.Memories)}
}

func (s *server) listRadioRevisions(w http.ResponseWriter, r *http.Request, a *app.Service) error {
	_, revs, err := a.RadioRevisions(ctx(r), r.PathValue("radio"))
	if err != nil {
		return err
	}
	out := make([]RadioRevision, len(revs))
	for i, rev := range revs {
		out[i] = newRadioRevision(rev)
	}
	writeJSON(w, http.StatusOK, map[string]any{"revisions": out})
	return nil
}

func (s *server) saveRadioRevision(w http.ResponseWriter, r *http.Request, a *app.Service) error {
	var in struct {
		Reason string `json:"reason"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	rev, err := a.SaveRadioRevision(ctx(r), r.PathValue("radio"), in.Reason)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, newRadioRevision(rev))
	return nil
}

func revisionParam(r *http.Request) (int, error) {
	n, err := strconv.Atoi(r.PathValue("n"))
	if err != nil || n < 1 {
		return 0, badRequest("a revision is a number from 1")
	}
	return n, nil
}

func (s *server) compareRadioRevision(w http.ResponseWriter, r *http.Request, a *app.Service) error {
	n, err := revisionParam(r)
	if err != nil {
		return err
	}
	rev, ch, err := a.CompareRevision(ctx(r), r.PathValue("radio"), n)
	if err != nil {
		return err
	}
	out := draftChangesOut(ch)
	out["channels"] = channelChanges(ch.Channels)
	writeJSON(w, http.StatusOK, map[string]any{"revision": newRadioRevision(rev), "changes": out})
	return nil
}

func (s *server) restoreRadioRevision(w http.ResponseWriter, r *http.Request, a *app.Service) error {
	n, err := revisionParam(r)
	if err != nil {
		return err
	}
	var in struct {
		ReplaceDraft bool `json:"replace_draft"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	d, err := a.RestoreRevision(ctx(r), r.PathValue("radio"), n, in.ReplaceDraft)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, Draft{d.ID, d.CreatedAt})
	return nil
}
