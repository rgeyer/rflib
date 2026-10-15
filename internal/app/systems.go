package app

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/ryan/rflib/internal/domain"
	"github.com/ryan/rflib/internal/store"
)

// Organizations, systems, and plans live in the shared library, so these
// operations reach it from any library's Service. Changes are maintainer
// operations: each records a revision with its reason.

// records returns the shared library, where organizations, systems, and
// plans are kept.
func (s *Service) records(ctx context.Context) (store.Library, error) {
	return s.sharedLibrary(ctx)
}

// Revision kinds for RecordHistory.
const (
	KindOrganization = "organization"
	KindSystem       = "system"
	KindPlan         = "plan"
)

// SharedChannelIDs resolves references to shared channels: IDs or ID
// prefixes in the shared list, or links in the Service's library, which
// stand for their shared channels.
func (s *Service) SharedChannelIDs(ctx context.Context, refs []string) ([]string, error) {
	sh, err := s.shared(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		id, err := sh.ChannelID(ctx, ref)
		if errors.Is(err, store.ErrNotFound) && sh != s {
			c, lerr := s.GetChannel(ctx, ref)
			if lerr == nil && c.Link != nil {
				id, err = c.Link.SharedID, nil
			} else if lerr == nil {
				err = domain.Invalidf("%s is a private channel; only shared channels can be in systems and plans", c.Name)
			}
		}
		if err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, nil
}

// OrgRef attaches an organization, by ID or name, in a role.
func (s *Service) OrgRef(ctx context.Context, ref string, role domain.OrgRole) (domain.OrgRef, error) {
	o, err := s.GetOrg(ctx, ref)
	if err != nil {
		return domain.OrgRef{}, err
	}
	return domain.OrgRef{OrgID: o.ID, Name: o.Name, Role: role}, nil
}

// RecordHistory returns an organization's, system's, or plan's revisions,
// oldest first.
func (s *Service) RecordHistory(ctx context.Context, kind, ref string) ([]domain.RecordRevision, error) {
	var id string
	switch kind {
	case KindOrganization:
		o, err := s.GetOrg(ctx, ref)
		if err != nil {
			return nil, err
		}
		id = o.ID
	case KindSystem:
		x, err := s.GetSystem(ctx, ref)
		if err != nil {
			return nil, err
		}
		id = x.ID
	case KindPlan:
		p, err := s.GetPlan(ctx, ref)
		if err != nil {
			return nil, err
		}
		id = p.ID
	default:
		return nil, domain.Invalidf("unknown kind %q", kind)
	}
	lib, err := s.records(ctx)
	if err != nil {
		return nil, err
	}
	return lib.RecordRevisions(ctx, kind, id)
}

// --- organizations ---

func (s *Service) CreateOrg(ctx context.Context, o domain.Organization, reason string) (domain.Organization, error) {
	lib, err := s.records(ctx)
	if err != nil {
		return o, err
	}
	return lib.CreateOrg(store.WithChange(ctx, reason, ""), o)
}

func (s *Service) GetOrg(ctx context.Context, ref string) (domain.Organization, error) {
	lib, err := s.records(ctx)
	if err != nil {
		return domain.Organization{}, err
	}
	return lib.GetOrg(ctx, ref)
}

func (s *Service) ListOrgs(ctx context.Context) ([]domain.Organization, error) {
	lib, err := s.records(ctx)
	if err != nil {
		return nil, err
	}
	return lib.ListOrgs(ctx)
}

func (s *Service) UpdateOrg(ctx context.Context, o domain.Organization, reason string) (domain.Organization, error) {
	lib, err := s.records(ctx)
	if err != nil {
		return o, err
	}
	return lib.UpdateOrg(store.WithChange(ctx, reason, ""), o)
}

// DeleteOrg deletes an organization attached to nothing.
func (s *Service) DeleteOrg(ctx context.Context, ref string) error {
	o, err := s.GetOrg(ctx, ref)
	if err != nil {
		return err
	}
	lib, err := s.records(ctx)
	if err != nil {
		return err
	}
	return lib.DeleteOrg(ctx, o.ID)
}

// OrgDetail is an organization and what it's attached to: shared channels
// and systems, in any role.
type OrgDetail struct {
	domain.Organization
	Channels []domain.Channel
	Systems  []domain.System
}

func (s *Service) OrgDetail(ctx context.Context, ref string) (OrgDetail, error) {
	o, err := s.GetOrg(ctx, ref)
	if err != nil {
		return OrgDetail{}, err
	}
	d := OrgDetail{Organization: o}
	lib, err := s.records(ctx)
	if err != nil {
		return d, err
	}
	has := func(rs []domain.OrgRef) bool {
		return slices.ContainsFunc(rs, func(r domain.OrgRef) bool { return r.OrgID == o.ID })
	}
	cs, err := lib.ListChannels(ctx)
	if err != nil {
		return d, err
	}
	for _, c := range cs {
		if has(c.Orgs) {
			d.Channels = append(d.Channels, c)
		}
	}
	ss, err := lib.ListSystems(ctx)
	if err != nil {
		return d, err
	}
	for _, x := range ss {
		if has(x.Orgs) {
			d.Systems = append(d.Systems, x)
		}
	}
	return d, nil
}

// --- systems ---

func (s *Service) CreateSystem(ctx context.Context, x domain.System, reason string) (domain.System, error) {
	lib, err := s.records(ctx)
	if err != nil {
		return x, err
	}
	return lib.CreateSystem(store.WithChange(ctx, reason, ""), x)
}

func (s *Service) GetSystem(ctx context.Context, ref string) (domain.System, error) {
	lib, err := s.records(ctx)
	if err != nil {
		return domain.System{}, err
	}
	return lib.GetSystem(ctx, ref)
}

func (s *Service) ListSystems(ctx context.Context) ([]domain.System, error) {
	lib, err := s.records(ctx)
	if err != nil {
		return nil, err
	}
	return lib.ListSystems(ctx)
}

func (s *Service) UpdateSystem(ctx context.Context, x domain.System, reason string) (domain.System, error) {
	lib, err := s.records(ctx)
	if err != nil {
		return x, err
	}
	return lib.UpdateSystem(store.WithChange(ctx, reason, ""), x)
}

func (s *Service) DeleteSystem(ctx context.Context, ref string) error {
	x, err := s.GetSystem(ctx, ref)
	if err != nil {
		return err
	}
	lib, err := s.records(ctx)
	if err != nil {
		return err
	}
	return lib.DeleteSystem(ctx, x.ID)
}

// SystemDetail is a system with its members' shared channels, in member
// order.
type SystemDetail struct {
	domain.System
	Channels []domain.Channel
}

func (s *Service) SystemDetail(ctx context.Context, ref string) (SystemDetail, error) {
	x, err := s.GetSystem(ctx, ref)
	if err != nil {
		return SystemDetail{}, err
	}
	d := SystemDetail{System: x}
	lib, err := s.records(ctx)
	if err != nil {
		return d, err
	}
	for _, m := range x.Members {
		c, err := lib.GetChannel(ctx, m.ChannelID)
		if err != nil {
			return d, err
		}
		d.Channels = append(d.Channels, c)
	}
	return d, nil
}

// systemChannels returns a system's shared channel IDs, for system:.
func (s *Service) systemChannels(ctx context.Context, ref string) (map[string]bool, error) {
	x, err := s.GetSystem(ctx, ref)
	if err != nil {
		return nil, err
	}
	lib, err := s.records(ctx)
	if err != nil {
		return nil, err
	}
	return lib.SystemChannelIDs(ctx, x.ID)
}

// --- plans ---

func (s *Service) CreatePlan(ctx context.Context, p domain.Plan, reason string) (domain.Plan, error) {
	lib, err := s.records(ctx)
	if err != nil {
		return p, err
	}
	if err := s.checkPlan(ctx, p); err != nil {
		return p, err
	}
	return lib.CreatePlan(store.WithChange(ctx, reason, ""), p)
}

func (s *Service) GetPlan(ctx context.Context, ref string) (domain.Plan, error) {
	lib, err := s.records(ctx)
	if err != nil {
		return domain.Plan{}, err
	}
	return lib.GetPlan(ctx, ref)
}

func (s *Service) ListPlans(ctx context.Context) ([]domain.Plan, error) {
	lib, err := s.records(ctx)
	if err != nil {
		return nil, err
	}
	return lib.ListPlans(ctx)
}

// UpdatePlan saves a plan and its entries after checking that it lays out
// (see PlanRows).
func (s *Service) UpdatePlan(ctx context.Context, p domain.Plan, reason string) (domain.Plan, error) {
	lib, err := s.records(ctx)
	if err != nil {
		return p, err
	}
	if err := s.checkPlan(ctx, p); err != nil {
		return p, err
	}
	return lib.UpdatePlan(store.WithChange(ctx, reason, ""), p)
}

// checkPlan refuses a plan whose entries don't lay out: a channel twice, a
// query that doesn't work, or a section start behind the entries before
// it.
func (s *Service) checkPlan(ctx context.Context, p domain.Plan) error {
	if err := domain.ValidatePlan(p); err != nil {
		return err
	}
	seen := map[string]bool{}
	for i, e := range p.Entries {
		if e.ChannelID == "" {
			continue
		}
		if seen[e.ChannelID] {
			return domain.Invalidf("entry %d: the channel is already in the plan", i+1)
		}
		seen[e.ChannelID] = true
	}
	_, err := s.PlanRows(ctx, p)
	return err
}

// PlanRow is one channel of a plan as ICS 205 lists it: its plan channel
// number, the entry it comes from (from 0), the shared channel, its name in
// the plan, and the entry's assignment and notes.
type PlanRow struct {
	Number     int
	Entry      int
	Channel    domain.Channel
	Name       string
	Assignment string
	Notes      string
}

// PlanRows lays a plan out: each channel entry, and each query section's
// matches in its sort order, numbered from 1 and jumping ahead at section
// starts. A query section leaves out channels the plan lists elsewhere.
func (s *Service) PlanRows(ctx context.Context, p domain.Plan) ([]PlanRow, error) {
	sh, err := s.shared(ctx)
	if err != nil {
		return nil, err
	}
	all, err := sh.lib.ListChannels(ctx)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]domain.Channel, len(all))
	for _, c := range all {
		byID[c.ID] = c
	}
	taken := map[string]bool{}
	for _, e := range p.Entries {
		if e.ChannelID != "" {
			taken[e.ChannelID] = true
		}
	}
	var rows []PlanRow
	next := 1
	for i, e := range p.Entries {
		var cs []domain.Channel
		if e.ChannelID != "" {
			c, ok := byID[e.ChannelID]
			if !ok {
				return nil, fmt.Errorf("entry %d: shared channel %q: %w", i+1, e.ChannelID, store.ErrNotFound)
			}
			cs = []domain.Channel{c}
		} else {
			found, err := sh.Find(ctx, e.Query)
			if err != nil {
				return nil, fmt.Errorf("entry %d: %w", i+1, err)
			}
			if err := domain.SortChannels(found, orDefault(e.Sort, "name")); err != nil {
				return nil, err
			}
			for _, c := range found {
				if !taken[c.ID] {
					taken[c.ID] = true
					cs = append(cs, c)
				}
			}
		}
		if e.SectionStart != nil {
			if *e.SectionStart < next {
				return nil, domain.Invalidf("entry %d starts at channel %d, but the entries before it already reach channel %d", i+1, *e.SectionStart, next-1)
			}
			next = *e.SectionStart
		}
		for _, c := range cs {
			name := c.Name
			if e.Name != "" {
				name = e.Name
			}
			rows = append(rows, PlanRow{Number: next, Entry: i, Channel: c, Name: name, Assignment: e.Assignment, Notes: e.Notes})
			next++
		}
	}
	return rows, nil
}
