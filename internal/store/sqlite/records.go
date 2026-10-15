package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ryan/rflib/internal/domain"
	"github.com/ryan/rflib/internal/store"
)

// Organizations, systems, and plans. Each write that changes a record's
// snapshot (domain.Snapshot) records a revision in record_revisions, with
// the reason in ctx (see store.WithChange).

// recordSnapshot stores a revision of a record when its snapshot differs
// from the latest one, and returns the record's revision number.
func (l *library) recordSnapshot(ctx context.Context, kind, id string, v any) (int, error) {
	data, err := domain.Snapshot(v)
	if err != nil {
		return 0, err
	}
	var last sql.NullInt64
	var lastData sql.NullString
	err = l.q.QueryRowContext(ctx, `SELECT revision, record FROM record_revisions WHERE kind=? AND record_id=? ORDER BY revision DESC LIMIT 1`, kind, id).Scan(&last, &lastData)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	if lastData.Valid && bytes.Equal([]byte(lastData.String), data) {
		return int(last.Int64), nil
	}
	n := int(last.Int64) + 1
	reason, author := store.ChangeOf(ctx)
	_, err = l.q.ExecContext(ctx, `INSERT INTO record_revisions (kind,record_id,revision,changed_at,reason,author,record) VALUES (?,?,?,?,?,?,?)`,
		kind, id, n, timestamp(time.Now().UTC()), reason, author, string(data))
	return n, err
}

func (l *library) RecordRevisions(ctx context.Context, kind, id string) ([]domain.RecordRevision, error) {
	rows, err := l.q.QueryContext(ctx, `SELECT revision,changed_at,reason,author,record FROM record_revisions WHERE kind=? AND record_id=? ORDER BY revision`, kind, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.RecordRevision
	for rows.Next() {
		var r domain.RecordRevision
		var at, data string
		if err := rows.Scan(&r.Number, &at, &r.Reason, &r.Author, &data); err != nil {
			return nil, err
		}
		r.At, r.Record = parseTime(at), []byte(data)
		out = append(out, r)
	}
	return out, rows.Err()
}

// --- organizations ---

const orgColumns = `id,name,kind,callsign,website,notes,revision,created_at,updated_at`

func prepareOrg(o *domain.Organization) error {
	o.Name = strings.TrimSpace(o.Name)
	if err := domain.ValidateOrganization(*o); err != nil {
		return err
	}
	o.Kind, _ = domain.ParseOrgKind(string(o.Kind))
	o.Callsign = strings.ToUpper(strings.TrimSpace(o.Callsign))
	return nil
}

func (l *library) CreateOrg(ctx context.Context, o domain.Organization) (domain.Organization, error) {
	if err := prepareOrg(&o); err != nil {
		return o, err
	}
	err := l.inTx(ctx, func(tx *library) error {
		if err := freeID(ctx, tx.q, "organizations", &o.ID); err != nil {
			return err
		}
		if err := assignIdentity(&o.ID, &o.CreatedAt, &o.UpdatedAt); err != nil {
			return err
		}
		var err error
		if o.Revision, err = tx.recordSnapshot(ctx, "organization", o.ID, o); err != nil {
			return err
		}
		_, err = tx.q.ExecContext(ctx, `INSERT INTO organizations (library_id,`+orgColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?)`,
			tx.id, o.ID, o.Name, string(o.Kind), o.Callsign, o.Website, o.Notes, o.Revision, timestamp(o.CreatedAt), timestamp(o.UpdatedAt))
		return conflict(err, "an organization named %q", o.Name)
	})
	return o, err
}

func (l *library) GetOrg(ctx context.Context, idOrName string) (domain.Organization, error) {
	o, err := scanOrg(l.q.QueryRowContext(ctx, `SELECT `+orgColumns+` FROM organizations WHERE library_id=? AND (id=? OR name=? COLLATE NOCASE)`, l.id, idOrName, idOrName))
	if errors.Is(err, sql.ErrNoRows) {
		return o, fmt.Errorf("organization %q: %w", idOrName, store.ErrNotFound)
	}
	return o, err
}

func (l *library) ListOrgs(ctx context.Context) ([]domain.Organization, error) {
	rows, err := l.q.QueryContext(ctx, `SELECT `+orgColumns+` FROM organizations WHERE library_id=? ORDER BY name`, l.id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Organization
	for rows.Next() {
		o, err := scanOrg(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func (l *library) UpdateOrg(ctx context.Context, o domain.Organization) (domain.Organization, error) {
	if err := prepareOrg(&o); err != nil {
		return o, err
	}
	err := l.inTx(ctx, func(tx *library) error {
		old, err := tx.GetOrg(ctx, o.ID)
		if err != nil {
			return err
		}
		o.ID, o.CreatedAt, o.UpdatedAt = old.ID, old.CreatedAt, time.Now().UTC()
		if o.Revision, err = tx.recordSnapshot(ctx, "organization", o.ID, o); err != nil {
			return err
		}
		_, err = tx.q.ExecContext(ctx, `UPDATE organizations SET name=?,kind=?,callsign=?,website=?,notes=?,revision=?,updated_at=? WHERE id=? AND library_id=?`,
			o.Name, string(o.Kind), o.Callsign, o.Website, o.Notes, o.Revision, timestamp(o.UpdatedAt), o.ID, tx.id)
		return conflict(err, "an organization named %q", o.Name)
	})
	return o, err
}

func (l *library) DeleteOrg(ctx context.Context, id string) error {
	return l.inTx(ctx, func(tx *library) error {
		if err := tx.owns(ctx, "organizations", "organization", id); err != nil {
			return err
		}
		var n int
		if err := tx.q.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM channel_orgs WHERE org_id=?) + (SELECT count(*) FROM system_orgs WHERE org_id=?)`, id, id).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return fmt.Errorf("organization %q is attached to %d channels and systems; take it off them first: %w", id, n, store.ErrConflict)
		}
		_, err := tx.q.ExecContext(ctx, `DELETE FROM organizations WHERE id=? AND library_id=?`, id, tx.id)
		return err
	})
}

func scanOrg(r scanner) (domain.Organization, error) {
	var o domain.Organization
	var created, updated string
	err := r.Scan(&o.ID, &o.Name, &o.Kind, &o.Callsign, &o.Website, &o.Notes, &o.Revision, &created, &updated)
	o.CreatedAt, o.UpdatedAt = parseTime(created), parseTime(updated)
	return o, err
}

// --- systems ---

const systemColumns = `id,name,link_type,notes,revision,created_at,updated_at`

// prepareSystem normalizes and checks a system, and that its members are
// this library's own channels.
func (tx *library) prepareSystem(ctx context.Context, s *domain.System) error {
	s.Name = strings.TrimSpace(s.Name)
	if err := domain.ValidateSystem(*s); err != nil {
		return err
	}
	s.Link, _ = domain.ParseLinkType(string(s.Link))
	s.Networks = domain.NormalizeNetworkIDs(s.Networks)
	for i := range s.Members {
		m := &s.Members[i]
		m.Linking, _ = domain.ParseLinking(string(m.Linking))
		var n int
		if err := tx.q.QueryRowContext(ctx, `SELECT count(*) FROM channels WHERE id=? AND library_id=? AND link_id IS NULL`, m.ChannelID, tx.id).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			return fmt.Errorf("channel %q: %w", m.ChannelID, store.ErrNotFound)
		}
	}
	var err error
	s.Orgs, err = tx.orgRefs(ctx, s.Orgs)
	return err
}

func (tx *library) writeSystemLists(ctx context.Context, s domain.System) error {
	if err := writeNetworks(ctx, tx.q, "system_networks", "system_id", s.ID, s.Networks); err != nil {
		return err
	}
	if err := writeOrgRefs(ctx, tx.q, "system_orgs", "system_id", s.ID, s.Orgs); err != nil {
		return err
	}
	if _, err := tx.q.ExecContext(ctx, `DELETE FROM system_members WHERE system_id=?`, s.ID); err != nil {
		return err
	}
	for _, m := range s.Members {
		if _, err := tx.q.ExecContext(ctx, `INSERT INTO system_members (system_id,channel_id,linking,notes) VALUES (?,?,?,?)`, s.ID, m.ChannelID, string(m.Linking), m.Notes); err != nil {
			return err
		}
	}
	return nil
}

func (l *library) CreateSystem(ctx context.Context, s domain.System) (domain.System, error) {
	err := l.inTx(ctx, func(tx *library) error {
		if err := tx.prepareSystem(ctx, &s); err != nil {
			return err
		}
		if err := freeID(ctx, tx.q, "systems", &s.ID); err != nil {
			return err
		}
		if err := assignIdentity(&s.ID, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return err
		}
		var err error
		if s.Revision, err = tx.recordSnapshot(ctx, "system", s.ID, s); err != nil {
			return err
		}
		_, err = tx.q.ExecContext(ctx, `INSERT INTO systems (library_id,`+systemColumns+`) VALUES (?,?,?,?,?,?,?,?)`,
			tx.id, s.ID, s.Name, string(s.Link), s.Notes, s.Revision, timestamp(s.CreatedAt), timestamp(s.UpdatedAt))
		if err != nil {
			return conflict(err, "a system named %q", s.Name)
		}
		return tx.writeSystemLists(ctx, s)
	})
	return s, err
}

func (l *library) GetSystem(ctx context.Context, idOrName string) (domain.System, error) {
	ss, err := l.listSystems(ctx, `s.library_id=? AND (s.id=? OR s.name=? COLLATE NOCASE)`, l.id, idOrName, idOrName)
	if err != nil {
		return domain.System{}, err
	}
	if len(ss) == 0 {
		return domain.System{}, fmt.Errorf("system %q: %w", idOrName, store.ErrNotFound)
	}
	return ss[0], nil
}

func (l *library) ListSystems(ctx context.Context) ([]domain.System, error) {
	return l.listSystems(ctx, `s.library_id=?`, l.id)
}

func (l *library) listSystems(ctx context.Context, where string, args ...any) ([]domain.System, error) {
	rows, err := l.q.QueryContext(ctx, `SELECT s.id,s.name,s.link_type,s.notes,s.revision,s.created_at,s.updated_at,`+
		networksSQL("system_networks", "system_id", "s.id")+`,`+orgsSQL("system_orgs", "system_id", "s.id")+
		` FROM systems s WHERE `+where+` ORDER BY s.name`, args...)
	if err != nil {
		return nil, err
	}
	var out []domain.System
	for rows.Next() {
		var s domain.System
		var created, updated string
		var networks, orgs sql.NullString
		if err := rows.Scan(&s.ID, &s.Name, &s.Link, &s.Notes, &s.Revision, &created, &updated, &networks, &orgs); err != nil {
			_ = rows.Close()
			return nil, err
		}
		s.CreatedAt, s.UpdatedAt = parseTime(created), parseTime(updated)
		s.Networks, s.Orgs = parseNetworks(networks), parseOrgRefs(orgs)
		out = append(out, s)
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		if out[i].Members, err = l.systemMembers(ctx, out[i].ID); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// systemMembers returns a system's members in their channels' name order.
func (l *library) systemMembers(ctx context.Context, id string) ([]domain.SystemMember, error) {
	rows, err := l.q.QueryContext(ctx, `SELECT m.channel_id,m.linking,m.notes,c.name FROM system_members m JOIN channels c ON c.id=m.channel_id WHERE m.system_id=?`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type member struct {
		domain.SystemMember
		name string
	}
	var ms []member
	for rows.Next() {
		var m member
		if err := rows.Scan(&m.ChannelID, &m.Linking, &m.Notes, &m.name); err != nil {
			return nil, err
		}
		ms = append(ms, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	slices.SortStableFunc(ms, func(a, b member) int { return domain.CompareNames(a.name, b.name) })
	out := make([]domain.SystemMember, len(ms))
	for i, m := range ms {
		out[i] = m.SystemMember
	}
	return out, nil
}

func (l *library) UpdateSystem(ctx context.Context, s domain.System) (domain.System, error) {
	err := l.inTx(ctx, func(tx *library) error {
		old, err := tx.GetSystem(ctx, s.ID)
		if err != nil {
			return err
		}
		if err := tx.prepareSystem(ctx, &s); err != nil {
			return err
		}
		s.ID, s.CreatedAt, s.UpdatedAt = old.ID, old.CreatedAt, time.Now().UTC()
		if s.Revision, err = tx.recordSnapshot(ctx, "system", s.ID, s); err != nil {
			return err
		}
		_, err = tx.q.ExecContext(ctx, `UPDATE systems SET name=?,link_type=?,notes=?,revision=?,updated_at=? WHERE id=? AND library_id=?`,
			s.Name, string(s.Link), s.Notes, s.Revision, timestamp(s.UpdatedAt), s.ID, tx.id)
		if err != nil {
			return conflict(err, "a system named %q", s.Name)
		}
		return tx.writeSystemLists(ctx, s)
	})
	return s, err
}

func (l *library) DeleteSystem(ctx context.Context, id string) error {
	res, err := l.q.ExecContext(ctx, `DELETE FROM systems WHERE id=? AND library_id=?`, id, l.id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("system %q: %w", id, store.ErrNotFound)
	}
	return nil
}

// SystemChannelIDs returns the channels in a system.
func (l *library) SystemChannelIDs(ctx context.Context, systemID string) (map[string]bool, error) {
	rows, err := l.q.QueryContext(ctx, `SELECT channel_id FROM system_members WHERE system_id=?`, systemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// --- plans ---

const planColumns = `id,name,description,starts_on,ends_on,status,revision,created_at,updated_at`

func (tx *library) preparePlan(ctx context.Context, p *domain.Plan) error {
	p.Name = strings.TrimSpace(p.Name)
	if err := domain.ValidatePlan(*p); err != nil {
		return err
	}
	p.Status, _ = domain.ParsePlanStatus(string(p.Status))
	for i := range p.Entries {
		e := &p.Entries[i]
		e.Name, e.Query = strings.TrimSpace(e.Name), strings.TrimSpace(e.Query)
		if e.Query != "" && e.Sort == "" {
			e.Sort = "name"
		}
		if e.ChannelID == "" {
			continue
		}
		var n int
		if err := tx.q.QueryRowContext(ctx, `SELECT count(*) FROM channels WHERE id=? AND library_id=? AND link_id IS NULL`, e.ChannelID, tx.id).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			return fmt.Errorf("channel %q: %w", e.ChannelID, store.ErrNotFound)
		}
	}
	return nil
}

func dateArg(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.Format(time.DateOnly)
}

func (tx *library) writePlanEntries(ctx context.Context, p domain.Plan) error {
	if _, err := tx.q.ExecContext(ctx, `DELETE FROM plan_entries WHERE plan_id=?`, p.ID); err != nil {
		return err
	}
	for i, e := range p.Entries {
		if _, err := tx.q.ExecContext(ctx, `INSERT INTO plan_entries (plan_id,position,channel_id,query,sort,name,assignment,notes,section_start) VALUES (?,?,?,?,?,?,?,?,?)`,
			p.ID, i, nullable(e.ChannelID), e.Query, e.Sort, e.Name, e.Assignment, e.Notes, intOrNil(e.SectionStart)); err != nil {
			return err
		}
	}
	return nil
}

func (l *library) CreatePlan(ctx context.Context, p domain.Plan) (domain.Plan, error) {
	err := l.inTx(ctx, func(tx *library) error {
		if err := tx.preparePlan(ctx, &p); err != nil {
			return err
		}
		if err := freeID(ctx, tx.q, "plans", &p.ID); err != nil {
			return err
		}
		if err := assignIdentity(&p.ID, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return err
		}
		var err error
		if p.Revision, err = tx.recordSnapshot(ctx, "plan", p.ID, p); err != nil {
			return err
		}
		_, err = tx.q.ExecContext(ctx, `INSERT INTO plans (library_id,`+planColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?)`,
			tx.id, p.ID, p.Name, p.Description, dateArg(p.Starts), dateArg(p.Ends), string(p.Status), p.Revision, timestamp(p.CreatedAt), timestamp(p.UpdatedAt))
		if err != nil {
			return conflict(err, "a plan named %q", p.Name)
		}
		return tx.writePlanEntries(ctx, p)
	})
	return p, err
}

func (l *library) GetPlan(ctx context.Context, idOrName string) (domain.Plan, error) {
	ps, err := l.listPlans(ctx, `library_id=? AND (id=? OR name=? COLLATE NOCASE)`, l.id, idOrName, idOrName)
	if err != nil {
		return domain.Plan{}, err
	}
	if len(ps) == 0 {
		return domain.Plan{}, fmt.Errorf("plan %q: %w", idOrName, store.ErrNotFound)
	}
	return ps[0], nil
}

func (l *library) ListPlans(ctx context.Context) ([]domain.Plan, error) {
	return l.listPlans(ctx, `library_id=?`, l.id)
}

// PlanByID returns a plan from any library, for radio sources, which use
// the shared library's plans.
func (l *library) PlanByID(ctx context.Context, id string) (domain.Plan, error) {
	ps, err := l.listPlans(ctx, `id=?`, id)
	if err != nil {
		return domain.Plan{}, err
	}
	if len(ps) == 0 {
		return domain.Plan{}, fmt.Errorf("plan %q: %w", id, store.ErrNotFound)
	}
	return ps[0], nil
}

func (l *library) listPlans(ctx context.Context, where string, args ...any) ([]domain.Plan, error) {
	rows, err := l.q.QueryContext(ctx, `SELECT `+planColumns+` FROM plans WHERE `+where+` ORDER BY name`, args...)
	if err != nil {
		return nil, err
	}
	var out []domain.Plan
	for rows.Next() {
		var p domain.Plan
		var starts, ends sql.NullString
		var created, updated string
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &starts, &ends, &p.Status, &p.Revision, &created, &updated); err != nil {
			_ = rows.Close()
			return nil, err
		}
		p.Starts, p.Ends = parseDate(starts), parseDate(ends)
		p.CreatedAt, p.UpdatedAt = parseTime(created), parseTime(updated)
		out = append(out, p)
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		if out[i].Entries, err = l.planEntries(ctx, out[i].ID); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func parseDate(s sql.NullString) *time.Time {
	if !s.Valid {
		return nil
	}
	t, err := time.Parse(time.DateOnly, s.String)
	if err != nil {
		return nil
	}
	return &t
}

func (l *library) planEntries(ctx context.Context, id string) ([]domain.PlanEntry, error) {
	rows, err := l.q.QueryContext(ctx, `SELECT channel_id,query,sort,name,assignment,notes,section_start FROM plan_entries WHERE plan_id=? ORDER BY position`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.PlanEntry
	for rows.Next() {
		var e domain.PlanEntry
		var channel sql.NullString
		var section sql.NullInt64
		if err := rows.Scan(&channel, &e.Query, &e.Sort, &e.Name, &e.Assignment, &e.Notes, &section); err != nil {
			return nil, err
		}
		e.ChannelID = channel.String
		if section.Valid {
			v := int(section.Int64)
			e.SectionStart = &v
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (l *library) UpdatePlan(ctx context.Context, p domain.Plan) (domain.Plan, error) {
	err := l.inTx(ctx, func(tx *library) error {
		old, err := tx.GetPlan(ctx, p.ID)
		if err != nil {
			return err
		}
		if err := tx.preparePlan(ctx, &p); err != nil {
			return err
		}
		p.ID, p.CreatedAt, p.UpdatedAt = old.ID, old.CreatedAt, time.Now().UTC()
		if p.Revision, err = tx.recordSnapshot(ctx, "plan", p.ID, p); err != nil {
			return err
		}
		_, err = tx.q.ExecContext(ctx, `UPDATE plans SET name=?,description=?,starts_on=?,ends_on=?,status=?,revision=?,updated_at=? WHERE id=? AND library_id=?`,
			p.Name, p.Description, dateArg(p.Starts), dateArg(p.Ends), string(p.Status), p.Revision, timestamp(p.UpdatedAt), p.ID, tx.id)
		if err != nil {
			return conflict(err, "a plan named %q", p.Name)
		}
		return tx.writePlanEntries(ctx, p)
	})
	return p, err
}
