package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/ryan/rflib/internal/app"
	"github.com/ryan/rflib/internal/domain"
)

// Organizations, systems, and plans live in the shared library, so their
// endpoints sit outside any library's path. Changes are maintainer
// operations and need a reason.

type OrgRef struct {
	OrgID string `json:"org_id"`
	Name  string `json:"name"`
	Role  string `json:"role"`
}

func newOrgRefs(rs []domain.OrgRef) []OrgRef {
	out := make([]OrgRef, len(rs))
	for i, r := range rs {
		out[i] = OrgRef{r.OrgID, r.Name, string(r.Role)}
	}
	return out
}

// orgRefInput attaches an organization, by ID or name, in a role (default
// operator).
type orgRefInput struct {
	OrgID string `json:"org_id"`
	Role  string `json:"role"`
}

func appOrgRefs(in []orgRefInput) ([]domain.OrgRef, error) {
	out := make([]domain.OrgRef, len(in))
	for i, r := range in {
		role, err := domain.ParseOrgRole(r.Role)
		if err != nil {
			return nil, err
		}
		out[i] = domain.OrgRef{OrgID: r.OrgID, Role: role}
	}
	return out, nil
}

func networkStrings(ns []domain.NetworkID) []string {
	out := make([]string, len(ns))
	for i, n := range ns {
		out[i] = n.String()
	}
	return out
}

type Organization struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Kind      string    `json:"kind"`
	Callsign  string    `json:"callsign"`
	Website   string    `json:"website"`
	Notes     string    `json:"notes"`
	Revision  int       `json:"revision"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func newOrg(o domain.Organization) Organization {
	return Organization{o.ID, o.Name, string(o.Kind), o.Callsign, o.Website, o.Notes, o.Revision, o.CreatedAt, o.UpdatedAt}
}

type SystemMember struct {
	ChannelID string `json:"channel_id"`
	Linking   string `json:"linking"`
	Notes     string `json:"notes"`
}

type System struct {
	ID            string         `json:"id"`
	Name          string         `json:"name"`
	Link          string         `json:"link"`
	Networks      []string       `json:"networks"`
	Organizations []OrgRef       `json:"organizations"`
	Members       []SystemMember `json:"members"`
	Notes         string         `json:"notes"`
	Revision      int            `json:"revision"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
}

func newSystem(x domain.System) System {
	ms := make([]SystemMember, len(x.Members))
	for i, m := range x.Members {
		ms[i] = SystemMember{m.ChannelID, string(m.Linking), m.Notes}
	}
	return System{x.ID, x.Name, string(x.Link), networkStrings(x.Networks), newOrgRefs(x.Orgs), ms, x.Notes, x.Revision, x.CreatedAt, x.UpdatedAt}
}

type PlanEntry struct {
	ChannelID    *string `json:"channel_id"`
	Query        string  `json:"query"`
	Sort         string  `json:"sort"`
	Name         string  `json:"name"`
	Assignment   string  `json:"assignment"`
	Notes        string  `json:"notes"`
	SectionStart *int    `json:"section_start"`
}

type Plan struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Starts      *string     `json:"starts"`
	Ends        *string     `json:"ends"`
	Status      string      `json:"status"`
	Entries     []PlanEntry `json:"entries"`
	Revision    int         `json:"revision"`
	CreatedAt   time.Time   `json:"created_at"`
	UpdatedAt   time.Time   `json:"updated_at"`
}

func dateString(t *time.Time) *string {
	if t == nil {
		return nil
	}
	return optional(t.Format(time.DateOnly))
}

func newPlan(p domain.Plan) Plan {
	es := make([]PlanEntry, len(p.Entries))
	for i, e := range p.Entries {
		es[i] = PlanEntry{optional(e.ChannelID), e.Query, e.Sort, e.Name, e.Assignment, e.Notes, e.SectionStart}
	}
	return Plan{p.ID, p.Name, p.Description, dateString(p.Starts), dateString(p.Ends), string(p.Status), es, p.Revision, p.CreatedAt, p.UpdatedAt}
}

// PlanRow is a plan's channel as ICS 205 lists it.
type PlanRow struct {
	Number     int     `json:"number"`
	Entry      int     `json:"entry"`
	Channel    Channel `json:"channel"`
	Name       string  `json:"name"`
	Assignment string  `json:"assignment"`
	Notes      string  `json:"notes"`
}

// RecordRevision is one version of an organization, system, or plan, with
// what changed from the one before.
type RecordRevision struct {
	Number  int       `json:"number"`
	At      time.Time `json:"at"`
	Reason  string    `json:"reason"`
	Author  string    `json:"author"`
	Changes []string  `json:"changes"`
}

// records returns the shared library's Service, where the records live.
func (s *server) records(r *http.Request) (*app.Service, error) {
	return app.Open(ctx(r), s.st, domain.SharedLibrary)
}

// recordHandler runs h with the shared library's Service.
func (s *server) recordHandler(h libHandler) handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		a, err := s.records(r)
		if err != nil {
			return err
		}
		return h(w, r, a)
	}
}

func needReason(reason string) error {
	if strings.TrimSpace(reason) == "" {
		return badRequest("a change needs a reason")
	}
	return nil
}

func (s *server) history(kind, param string) libHandler {
	return func(w http.ResponseWriter, r *http.Request, a *app.Service) error {
		revs, err := a.RecordHistory(ctx(r), kind, r.PathValue(param))
		if err != nil {
			return err
		}
		out := make([]RecordRevision, len(revs))
		for i, rev := range revs {
			changes := []string{}
			if i > 0 {
				changes = nonNil(domain.RecordChanges(revs[i-1].Record, rev.Record))
			}
			out[i] = RecordRevision{rev.Number, rev.At, rev.Reason, rev.Author, changes}
		}
		writeJSON(w, http.StatusOK, map[string]any{"revisions": out})
		return nil
	}
}

// --- organizations ---

type orgInput struct {
	Name     *string `json:"name"`
	Kind     *string `json:"kind"`
	Callsign *string `json:"callsign"`
	Website  *string `json:"website"`
	Notes    *string `json:"notes"`
	Reason   string  `json:"reason"`
}

func (in orgInput) apply(o *domain.Organization) error {
	if in.Kind != nil {
		k, err := domain.ParseOrgKind(*in.Kind)
		if err != nil {
			return err
		}
		o.Kind = k
	}
	for _, f := range []struct {
		v   *string
		dst *string
	}{{in.Name, &o.Name}, {in.Callsign, &o.Callsign}, {in.Website, &o.Website}, {in.Notes, &o.Notes}} {
		if f.v != nil {
			*f.dst = *f.v
		}
	}
	return needReason(in.Reason)
}

func (s *server) listOrgs(w http.ResponseWriter, r *http.Request, a *app.Service) error {
	orgs, err := a.ListOrgs(ctx(r))
	if err != nil {
		return err
	}
	out := make([]Organization, len(orgs))
	for i, o := range orgs {
		out[i] = newOrg(o)
	}
	writeJSON(w, http.StatusOK, map[string]any{"organizations": out})
	return nil
}

func (s *server) createOrg(w http.ResponseWriter, r *http.Request, a *app.Service) error {
	var in orgInput
	if err := readJSON(r, &in); err != nil {
		return err
	}
	var o domain.Organization
	if err := in.apply(&o); err != nil {
		return err
	}
	o, err := a.CreateOrg(ctx(r), o, in.Reason)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, newOrg(o))
	return nil
}

func (s *server) getOrg(w http.ResponseWriter, r *http.Request, a *app.Service) error {
	d, err := a.OrgDetail(ctx(r), r.PathValue("org"))
	if err != nil {
		return err
	}
	systems := make([]System, len(d.Systems))
	for i, x := range d.Systems {
		systems[i] = newSystem(x)
	}
	writeJSON(w, http.StatusOK, map[string]any{"organization": newOrg(d.Organization), "channels": newChannels(d.Channels), "systems": systems})
	return nil
}

func (s *server) updateOrg(w http.ResponseWriter, r *http.Request, a *app.Service) error {
	var in orgInput
	if err := readJSON(r, &in); err != nil {
		return err
	}
	o, err := a.GetOrg(ctx(r), r.PathValue("org"))
	if err != nil {
		return err
	}
	if err := in.apply(&o); err != nil {
		return err
	}
	if o, err = a.UpdateOrg(ctx(r), o, in.Reason); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, newOrg(o))
	return nil
}

func (s *server) deleteOrg(w http.ResponseWriter, r *http.Request, a *app.Service) error {
	if err := a.DeleteOrg(ctx(r), r.PathValue("org")); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// --- systems ---

type systemInput struct {
	Name          *string         `json:"name"`
	Link          *string         `json:"link"`
	Networks      *[]string       `json:"networks"`
	Organizations *[]orgRefInput  `json:"organizations"`
	Members       *[]SystemMember `json:"members"`
	Notes         *string         `json:"notes"`
	Reason        string          `json:"reason"`
}

func (in systemInput) apply(r *http.Request, a *app.Service, x *domain.System) error {
	if in.Name != nil {
		x.Name = *in.Name
	}
	if in.Notes != nil {
		x.Notes = *in.Notes
	}
	if in.Link != nil {
		t, err := domain.ParseLinkType(*in.Link)
		if err != nil {
			return err
		}
		x.Link = t
	}
	if in.Networks != nil {
		ns, err := domain.ParseNetworkIDs(*in.Networks)
		if err != nil {
			return err
		}
		x.Networks = ns
	}
	if in.Organizations != nil {
		rs, err := appOrgRefs(*in.Organizations)
		if err != nil {
			return err
		}
		x.Orgs = rs
	}
	if in.Members != nil {
		x.Members = nil
		for _, m := range *in.Members {
			ids, err := a.SharedChannelIDs(ctx(r), []string{m.ChannelID})
			if err != nil {
				return err
			}
			linking, err := domain.ParseLinking(m.Linking)
			if err != nil {
				return err
			}
			x.Members = append(x.Members, domain.SystemMember{ChannelID: ids[0], Linking: linking, Notes: m.Notes})
		}
	}
	return needReason(in.Reason)
}

func (s *server) listSystems(w http.ResponseWriter, r *http.Request, a *app.Service) error {
	xs, err := a.ListSystems(ctx(r))
	if err != nil {
		return err
	}
	out := make([]System, len(xs))
	for i, x := range xs {
		out[i] = newSystem(x)
	}
	writeJSON(w, http.StatusOK, map[string]any{"systems": out})
	return nil
}

func (s *server) createSystem(w http.ResponseWriter, r *http.Request, a *app.Service) error {
	var in systemInput
	if err := readJSON(r, &in); err != nil {
		return err
	}
	var x domain.System
	if err := in.apply(r, a, &x); err != nil {
		return err
	}
	x, err := a.CreateSystem(ctx(r), x, in.Reason)
	if err != nil {
		return err
	}
	return s.writeSystem(w, r, a, http.StatusCreated, x.ID)
}

// writeSystem writes a system with its members' channels.
func (s *server) writeSystem(w http.ResponseWriter, r *http.Request, a *app.Service, status int, ref string) error {
	d, err := a.SystemDetail(ctx(r), ref)
	if err != nil {
		return err
	}
	writeJSON(w, status, map[string]any{"system": newSystem(d.System), "channels": newChannels(d.Channels)})
	return nil
}

func (s *server) getSystem(w http.ResponseWriter, r *http.Request, a *app.Service) error {
	return s.writeSystem(w, r, a, http.StatusOK, r.PathValue("system"))
}

func (s *server) updateSystem(w http.ResponseWriter, r *http.Request, a *app.Service) error {
	var in systemInput
	if err := readJSON(r, &in); err != nil {
		return err
	}
	x, err := a.GetSystem(ctx(r), r.PathValue("system"))
	if err != nil {
		return err
	}
	if err := in.apply(r, a, &x); err != nil {
		return err
	}
	if _, err := a.UpdateSystem(ctx(r), x, in.Reason); err != nil {
		return err
	}
	return s.writeSystem(w, r, a, http.StatusOK, x.ID)
}

func (s *server) deleteSystem(w http.ResponseWriter, r *http.Request, a *app.Service) error {
	if err := a.DeleteSystem(ctx(r), r.PathValue("system")); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// --- plans ---

type planInput struct {
	Name        *string         `json:"name"`
	Description *string         `json:"description"`
	Starts      json.RawMessage `json:"starts"`
	Ends        json.RawMessage `json:"ends"`
	Status      *string         `json:"status"`
	Entries     *[]PlanEntry    `json:"entries"`
	Reason      string          `json:"reason"`
}

func decodeDate(key string, v json.RawMessage) (*time.Time, error) {
	if isNull(v) {
		return nil, nil
	}
	var s string
	if err := json.Unmarshal(v, &s); err != nil {
		return nil, badRequest("%s: %v", key, err)
	}
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return nil, badRequest("%s must be a date, YYYY-MM-DD", key)
	}
	return &t, nil
}

func (in planInput) apply(r *http.Request, a *app.Service, p *domain.Plan) error {
	if in.Name != nil {
		p.Name = *in.Name
	}
	if in.Description != nil {
		p.Description = *in.Description
	}
	if in.Status != nil {
		st, err := domain.ParsePlanStatus(*in.Status)
		if err != nil {
			return err
		}
		p.Status = st
	}
	for _, f := range []struct {
		key string
		v   json.RawMessage
		dst **time.Time
	}{{"starts", in.Starts, &p.Starts}, {"ends", in.Ends, &p.Ends}} {
		if f.v != nil {
			t, err := decodeDate(f.key, f.v)
			if err != nil {
				return err
			}
			*f.dst = t
		}
	}
	if in.Entries != nil {
		p.Entries = nil
		for _, e := range *in.Entries {
			de := domain.PlanEntry{Query: e.Query, Sort: e.Sort, Name: e.Name, Assignment: e.Assignment, Notes: e.Notes, SectionStart: e.SectionStart}
			if e.ChannelID != nil && *e.ChannelID != "" {
				ids, err := a.SharedChannelIDs(ctx(r), []string{*e.ChannelID})
				if err != nil {
					return err
				}
				de.ChannelID = ids[0]
			}
			p.Entries = append(p.Entries, de)
		}
	}
	return needReason(in.Reason)
}

func (s *server) listPlans(w http.ResponseWriter, r *http.Request, a *app.Service) error {
	ps, err := a.ListPlans(ctx(r))
	if err != nil {
		return err
	}
	out := make([]Plan, len(ps))
	for i, p := range ps {
		out[i] = newPlan(p)
	}
	writeJSON(w, http.StatusOK, map[string]any{"plans": out})
	return nil
}

// writePlan writes a plan with its rows, as ICS 205 lists them.
func (s *server) writePlan(w http.ResponseWriter, r *http.Request, a *app.Service, status int, ref string) error {
	p, err := a.GetPlan(ctx(r), ref)
	if err != nil {
		return err
	}
	rows, err := a.PlanRows(ctx(r), p)
	if err != nil {
		return err
	}
	out := make([]PlanRow, len(rows))
	for i, row := range rows {
		out[i] = PlanRow{row.Number, row.Entry + 1, newChannel(row.Channel), row.Name, row.Assignment, row.Notes}
	}
	writeJSON(w, status, map[string]any{"plan": newPlan(p), "rows": out})
	return nil
}

func (s *server) createPlan(w http.ResponseWriter, r *http.Request, a *app.Service) error {
	var in planInput
	if err := readJSON(r, &in); err != nil {
		return err
	}
	var p domain.Plan
	if err := in.apply(r, a, &p); err != nil {
		return err
	}
	p, err := a.CreatePlan(ctx(r), p, in.Reason)
	if err != nil {
		return err
	}
	return s.writePlan(w, r, a, http.StatusCreated, p.ID)
}

func (s *server) getPlan(w http.ResponseWriter, r *http.Request, a *app.Service) error {
	return s.writePlan(w, r, a, http.StatusOK, r.PathValue("plan"))
}

func (s *server) updatePlan(w http.ResponseWriter, r *http.Request, a *app.Service) error {
	var in planInput
	if err := readJSON(r, &in); err != nil {
		return err
	}
	p, err := a.GetPlan(ctx(r), r.PathValue("plan"))
	if err != nil {
		return err
	}
	if err := in.apply(r, a, &p); err != nil {
		return err
	}
	if _, err := a.UpdatePlan(ctx(r), p, in.Reason); err != nil {
		return err
	}
	return s.writePlan(w, r, a, http.StatusOK, p.ID)
}
