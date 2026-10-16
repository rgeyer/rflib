package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ryan/rflib/internal/domain"
	"github.com/ryan/rflib/internal/store"
)

// prepareChannel normalizes and validates a channel.
func prepareChannel(c *domain.Channel) error {
	c.Mode = strings.ToUpper(strings.TrimSpace(c.Mode))
	if err := domain.ValidateChannel(*c); err != nil {
		return err
	}
	c.Status = c.Status.OrActive()
	c.Tags = domain.NormalizeTags(c.Tags)
	c.ShortNames = domain.NormalizeShortNames(c.Name, c.ShortNames)
	c.Networks = domain.NormalizeNetworkIDs(c.Networks)
	return nil
}

// shared reports whether this is the shared library.
func (l *library) shared(ctx context.Context) (bool, error) {
	var kind string
	err := l.q.QueryRowContext(ctx, `SELECT kind FROM libraries WHERE id=?`, l.id).Scan(&kind)
	if errors.Is(err, sql.ErrNoRows) {
		return false, fmt.Errorf("library %q: %w", l.id, store.ErrNotFound)
	}
	return kind == sharedKind, err
}

func (l *library) CreateChannel(ctx context.Context, c domain.Channel) (domain.Channel, error) {
	if c.Link != nil {
		return l.SaveAnnotation(ctx, c)
	}
	err := l.inTx(ctx, func(tx *library) error {
		if err := freeID(ctx, tx.q, "channels", &c.ID); err != nil {
			return err
		}
		if err := assignIdentity(&c.ID, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return err
		}
		var err error
		c, err = tx.writeChannel(ctx, c, nil)
		return err
	})
	return c, err
}

func (l *library) UpdateChannel(ctx context.Context, c domain.Channel) (domain.Channel, error) {
	err := l.inTx(ctx, func(tx *library) error {
		old, err := tx.GetChannel(ctx, c.ID)
		if err != nil {
			return err
		}
		if old.Link != nil {
			// A view of a shared channel: save the user's annotation.
			c.Link = &domain.Link{SharedID: old.Link.SharedID}
			c, err = tx.SaveAnnotation(ctx, c)
			return err
		}
		c.CreatedAt, c.UpdatedAt = old.CreatedAt, time.Now().UTC()
		c, err = tx.writeChannel(ctx, c, &old)
		return err
	})
	return c, err
}

// writeChannel inserts c, or with old updates it. A link takes its shared
// fields from its shared channel, whatever c says. A shared channel whose
// shared fields change gets a new revision, recorded with the reason in
// ctx (see store.WithChange).
func (tx *library) writeChannel(ctx context.Context, c domain.Channel, old *domain.Channel) (domain.Channel, error) {
	isShared, err := tx.shared(ctx)
	if err != nil {
		return c, err
	}
	if c.Link != nil && isShared {
		return c, domain.Invalidf("a shared channel can't link to another")
	}
	if err := prepareChannel(&c); err != nil {
		return c, err
	}
	if c.Link == nil {
		if c.Orgs, err = tx.orgRefs(ctx, c.Orgs); err != nil {
			return c, err
		}
	}
	record := false
	if isShared {
		switch {
		case old == nil:
			c.Revision, record = 1, true
		case len(domain.SharedChanges(*old, c)) > 0:
			c.Revision, record = old.Revision+1, true
		default:
			c.Revision = old.Revision
		}
	} else if c.Link == nil {
		// A private channel records a revision whenever anything it
		// records changes.
		switch {
		case old == nil:
			c.Revision, record = 1, true
		case !sameRecord(*old, c):
			c.Revision, record = old.Revision+1, true
		default:
			c.Revision = old.Revision
		}
	}
	var linkID any
	if c.Link != nil {
		linkID = c.Link.SharedID
	}
	seen := 0
	var backOn any
	if c.BackOn != nil {
		backOn = c.BackOn.Format(time.DateOnly)
	}
	rt, tt := toneArgs(c.ReceiveTone), toneArgs(c.TransmitTone)
	lat, lon, radius := locationArgs(c.Location)
	// A link row keeps a copy of its shared fields, which reads ignore.
	args := []any{lat, lon, radius, c.Name, c.ReceiveHz, c.TransmitHz, c.Mode, string(c.Service), c.RXOnly, rt.kind, rt.ctcss, rt.dcs, rt.inv, tt.kind, tt.ctcss, tt.dcs, tt.inv,
		c.ToneSquelch, string(c.Power), c.Bandwidth, c.Skip, c.Notes, linkID, seen, string(c.Status), c.StatusNote, backOn, nullable(c.ReplacedBy), c.Revision, c.CopiedFrom, timestamp(c.UpdatedAt)}
	if old == nil {
		_, err = tx.q.ExecContext(ctx, `INSERT INTO channels (`+writeColumns+`,id,library_id,created_at) VALUES (`+strings.Repeat("?,", len(args)+2)+`?)`,
			append(args, c.ID, tx.id, timestamp(c.CreatedAt))...)
		if err != nil {
			return c, fmt.Errorf("create channel: %w", err)
		}
	} else {
		set := strings.Join(strings.Split(writeColumns, ","), "=?,") + "=?"
		r, err := tx.q.ExecContext(ctx, `UPDATE channels SET `+set+` WHERE id=? AND library_id=?`, append(args, c.ID, tx.id)...)
		if err != nil {
			return c, fmt.Errorf("update channel: %w", err)
		}
		if n, _ := r.RowsAffected(); n == 0 {
			return c, fmt.Errorf("channel %q: %w", c.ID, store.ErrNotFound)
		}
	}
	// A link's own tags are the user's; the shared ones stay on the link.
	if err := writeChannelLists(ctx, tx.q, c); err != nil {
		return c, err
	}
	// A link's networks and organizations are its shared channel's.
	if c.Link == nil {
		if err := writeChannelRefs(ctx, tx.q, c); err != nil {
			return c, err
		}
	}
	if record {
		if err := tx.recordRevision(ctx, c); err != nil {
			return c, err
		}
	}
	return c, nil
}

const writeColumns = `latitude,longitude,radius_km,name,receive_hz,transmit_hz,mode,service,rx_only,receive_tone_kind,receive_ctcss_millihz,receive_dcs_code,receive_dcs_inverted,transmit_tone_kind,transmit_ctcss_millihz,transmit_dcs_code,transmit_dcs_inverted,tone_squelch,power,bandwidth,skip,notes,link_id,seen_revision,status,status_note,back_on,replaced_by,revision,copied_from,updated_at`

// sharedChannel returns a channel of the shared library, not itself a link.
func (l *library) sharedChannel(ctx context.Context, id string) (domain.Channel, error) {
	cs, err := l.listChannels(ctx, `c.id=? AND c.link_id IS NULL AND c.library_id IN (SELECT id FROM libraries WHERE kind='`+sharedKind+`')`, id)
	if err != nil {
		return domain.Channel{}, err
	}
	if len(cs) == 0 {
		return domain.Channel{}, fmt.Errorf("shared channel %q: %w", id, store.ErrNotFound)
	}
	return cs[0], nil
}

// writeChannelLists replaces a channel's tags, and its short names in the
// user's order.
func writeChannelLists(ctx context.Context, ex execer, c domain.Channel) error {
	if _, err := ex.ExecContext(ctx, `DELETE FROM channel_tags WHERE channel_id=?`, c.ID); err != nil {
		return err
	}
	for _, tag := range c.Tags {
		if _, err := ex.ExecContext(ctx, `INSERT INTO channel_tags(channel_id,tag) VALUES(?,?)`, c.ID, tag); err != nil {
			return err
		}
	}
	if _, err := ex.ExecContext(ctx, `DELETE FROM channel_short_names WHERE channel_id=?`, c.ID); err != nil {
		return err
	}
	for i, name := range c.ShortNames {
		if _, err := ex.ExecContext(ctx, `INSERT INTO channel_short_names(channel_id,position,name) VALUES(?,?,?)`, c.ID, i, name); err != nil {
			return err
		}
	}
	return nil
}

// writeChannelRefs replaces a channel's network identifiers and
// organizations.
func writeChannelRefs(ctx context.Context, ex execer, c domain.Channel) error {
	if err := writeNetworks(ctx, ex, "channel_networks", "channel_id", c.ID, c.Networks); err != nil {
		return err
	}
	return writeOrgRefs(ctx, ex, "channel_orgs", "channel_id", c.ID, c.Orgs)
}

func writeNetworks(ctx context.Context, ex execer, table, key, id string, ns []domain.NetworkID) error {
	if _, err := ex.ExecContext(ctx, `DELETE FROM `+table+` WHERE `+key+`=?`, id); err != nil {
		return err
	}
	for _, n := range ns {
		if _, err := ex.ExecContext(ctx, `INSERT INTO `+table+`(`+key+`,network,node) VALUES(?,?,?)`, id, string(n.Network), n.Node); err != nil {
			return err
		}
	}
	return nil
}

func writeOrgRefs(ctx context.Context, ex execer, table, key, id string, rs []domain.OrgRef) error {
	if _, err := ex.ExecContext(ctx, `DELETE FROM `+table+` WHERE `+key+`=?`, id); err != nil {
		return err
	}
	for _, r := range rs {
		if _, err := ex.ExecContext(ctx, `INSERT INTO `+table+`(`+key+`,org_id,role) VALUES(?,?,?)`, id, r.OrgID, string(r.Role)); err != nil {
			return err
		}
	}
	return nil
}

// orgRefs checks that the organizations are this library's, fills in their
// names, and normalizes the list.
func (l *library) orgRefs(ctx context.Context, rs []domain.OrgRef) ([]domain.OrgRef, error) {
	out := make([]domain.OrgRef, len(rs))
	for i, r := range rs {
		o, err := l.GetOrg(ctx, r.OrgID)
		if err != nil {
			return nil, err
		}
		role, err := domain.ParseOrgRole(string(r.Role))
		if err != nil {
			return nil, err
		}
		out[i] = domain.OrgRef{OrgID: o.ID, Name: o.Name, Role: role}
	}
	return domain.NormalizeOrgRefs(out), nil
}

// parseNetworks and parseOrgRefs read the lists the list query joins.
func parseNetworks(s sql.NullString) []domain.NetworkID {
	if !s.Valid || s.String == "" {
		return nil
	}
	var out []domain.NetworkID
	for _, v := range strings.Split(s.String, listSeparator) {
		net, node, _ := strings.Cut(v, ":")
		out = append(out, domain.NetworkID{Network: domain.Network(net), Node: node})
	}
	return domain.NormalizeNetworkIDs(out)
}

func parseOrgRefs(s sql.NullString) []domain.OrgRef {
	if !s.Valid || s.String == "" {
		return nil
	}
	var out []domain.OrgRef
	for _, v := range strings.Split(s.String, listSeparator) {
		p := strings.SplitN(v, "\x1e", 3)
		if len(p) == 3 {
			out = append(out, domain.OrgRef{Role: domain.OrgRole(p[0]), OrgID: p[1], Name: p[2]})
		}
	}
	return domain.NormalizeOrgRefs(out)
}

// networksSQL and orgsSQL select an owner's network identifiers and
// organizations as one text each; see parseNetworks and parseOrgRefs.
func networksSQL(table, key, owner string) string {
	return `(SELECT group_concat(w.network || ':' || w.node, char(31)) FROM ` + table + ` w WHERE w.` + key + `=` + owner + `)`
}

func orgsSQL(table, key, owner string) string {
	return `(SELECT group_concat(o.role || char(30) || o.org_id || char(30) || g.name, char(31)) FROM ` + table + ` o JOIN organizations g ON g.id=o.org_id WHERE o.` + key + `=` + owner + `)`
}

func locationArgs(l *domain.Location) (lat, lon, radius any) {
	if l == nil {
		return nil, nil, nil
	}
	if l.RadiusKM != nil {
		radius = *l.RadiusKM
	}
	return l.Latitude, l.Longitude, radius
}

type toneArg struct {
	kind  string
	ctcss any
	dcs   any
	inv   bool
}

func toneArgs(t domain.Tone) toneArg {
	a := toneArg{kind: string(t.Kind), inv: t.Inverted}
	if t.CTCSSMilliHz != nil {
		a.ctcss = *t.CTCSSMilliHz
	}
	if t.DCSCode != nil {
		a.dcs = *t.DCSCode
	}
	return a
}

func (l *library) DeleteChannel(ctx context.Context, id string) error {
	isShared, err := l.shared(ctx)
	if err != nil {
		return err
	}
	if isShared {
		var used int
		if err := l.q.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM channels WHERE link_id=?) + (SELECT count(*) FROM radio_channels WHERE channel_id=?)`, id, id).Scan(&used); err != nil {
			return err
		}
		if used > 0 {
			return fmt.Errorf("channel %q is on radios or annotated in libraries; retire it instead: %w", id, store.ErrConflict)
		}
	} else if r, err := l.q.ExecContext(ctx, `DELETE FROM channels WHERE link_id=? AND library_id=?`, id, l.id); err != nil {
		return fmt.Errorf("delete annotation: %w", err)
	} else if n, _ := r.RowsAffected(); n > 0 {
		// A view of a shared channel: only the user's annotation goes, and
		// its history records that.
		return l.recordNotes(ctx, id, notesRecord{})
	}
	r, err := l.q.ExecContext(ctx, `DELETE FROM channels WHERE id=? AND library_id=? AND link_id IS NULL`, id, l.id)
	if err != nil {
		return fmt.Errorf("delete channel: %w", err)
	}
	if n, _ := r.RowsAffected(); n == 0 {
		return fmt.Errorf("channel %q: %w", id, store.ErrNotFound)
	}
	return nil
}

// GetChannel returns one of the library's own channels, or, outside the
// shared library, its view of a shared channel (see SharedViews).
func (l *library) GetChannel(ctx context.Context, id string) (domain.Channel, error) {
	chs, err := l.listChannels(ctx, `c.library_id=? AND c.id=? AND c.link_id IS NULL`, l.id, id)
	if err != nil {
		return domain.Channel{}, err
	}
	if len(chs) > 0 {
		return chs[0], nil
	}
	isShared, err := l.shared(ctx)
	if err != nil {
		return domain.Channel{}, err
	}
	if !isShared {
		views, err := l.SharedViews(ctx, []string{id})
		if err != nil {
			return domain.Channel{}, err
		}
		if v, ok := views[id]; ok {
			return v, nil
		}
	}
	return domain.Channel{}, fmt.Errorf("channel %q: %w", id, store.ErrNotFound)
}

func (l *library) ListChannels(ctx context.Context) ([]domain.Channel, error) {
	return l.listChannels(ctx, `c.library_id=?`, l.id)
}

// listSeparator joins a channel's tags and short names in the list query.
// The ASCII unit separator can't appear in one typed or imported as text.
const listSeparator = "\x1f"

// listChannels returns the channels matching where, a SQL condition on c,
// with links resolved to their shared channels.
func (l *library) listChannels(ctx context.Context, where string, args ...any) ([]domain.Channel, error) {
	q := `SELECT c.id,c.latitude,c.longitude,c.radius_km,c.name,c.receive_hz,c.transmit_hz,c.mode,c.service,c.rx_only,c.receive_tone_kind,c.receive_ctcss_millihz,c.receive_dcs_code,c.receive_dcs_inverted,c.transmit_tone_kind,c.transmit_ctcss_millihz,c.transmit_dcs_code,c.transmit_dcs_inverted,c.tone_squelch,c.power,c.bandwidth,c.skip,c.notes,c.link_id,c.seen_revision,c.status,c.status_note,c.back_on,c.replaced_by,c.revision,c.copied_from,c.created_at,c.updated_at,(SELECT group_concat(t.tag, char(31) ORDER BY t.tag) FROM channel_tags t WHERE t.channel_id=c.id),(SELECT group_concat(n.name, char(31) ORDER BY n.position) FROM channel_short_names n WHERE n.channel_id=c.id),` + networksSQL("channel_networks", "channel_id", "c.id") + `,` + orgsSQL("channel_orgs", "channel_id", "c.id") + ` FROM channels c WHERE ` + where + ` ORDER BY c.name,c.receive_hz`
	rows, err := l.q.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list channels: %w", err)
	}
	var out []domain.Channel
	for rows.Next() {
		c, err := scanChannel(rows)
		if err != nil {
			_ = rows.Close()
			return nil, err
		}
		out = append(out, c)
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := l.resolveLinks(ctx, out); err != nil {
		return nil, err
	}
	// SQL's order breaks ties stably; numbers in names need Go's.
	slices.SortStableFunc(out, domain.CompareChannels)
	return out, nil
}

// resolveLinks fills in links' shared fields.
func (l *library) resolveLinks(ctx context.Context, cs []domain.Channel) error {
	var ids []any
	for _, c := range cs {
		if c.Link != nil {
			ids = append(ids, c.Link.SharedID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	shared, err := l.listChannels(ctx, `c.link_id IS NULL AND c.id IN (`+strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")+`)`, ids...)
	if err != nil {
		return err
	}
	byID := make(map[string]domain.Channel, len(shared))
	for _, s := range shared {
		byID[s.ID] = s
	}
	seen, err := l.seen(ctx, byID)
	if err != nil {
		return err
	}
	for i, c := range cs {
		if c.Link != nil {
			cs[i] = domain.Linked(c, byID[c.Link.SharedID], seen[c.Link.SharedID])
		}
	}
	return nil
}

// SharedViews returns the library's views of the given shared channels,
// by ID: each shared channel with the library's annotation, if any, laid
// over it (see domain.Linked). IDs that aren't shared channels are left
// out.
func (l *library) SharedViews(ctx context.Context, ids []string) (map[string]domain.Channel, error) {
	out := map[string]domain.Channel{}
	if len(ids) == 0 {
		return out, nil
	}
	marks := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	annotated, err := l.listChannels(ctx, `c.library_id=? AND c.link_id IN (`+marks+`)`, append([]any{l.id}, args...)...)
	if err != nil {
		return nil, err
	}
	for _, v := range annotated {
		out[v.ID] = v
	}
	shared, err := l.listChannels(ctx, `c.link_id IS NULL AND c.library_id IN (SELECT id FROM libraries WHERE kind='`+sharedKind+`') AND c.id IN (`+marks+`)`, args...)
	if err != nil {
		return nil, err
	}
	byID := map[string]domain.Channel{}
	for _, c := range shared {
		if _, ok := out[c.ID]; !ok {
			byID[c.ID] = c
		}
	}
	seen, err := l.seen(ctx, byID)
	if err != nil {
		return nil, err
	}
	for id, c := range byID {
		out[id] = domain.Linked(domain.Channel{}, c, seen[id])
	}
	return out, nil
}

// seen returns the revision the library last acknowledged for each shared
// channel: its follow's, or the channel's current revision when it isn't
// followed yet.
func (l *library) seen(ctx context.Context, shared map[string]domain.Channel) (map[string]int, error) {
	out := make(map[string]int, len(shared))
	for id, c := range shared {
		out[id] = c.Revision
	}
	if len(shared) == 0 {
		return out, nil
	}
	rows, err := l.q.QueryContext(ctx, `SELECT shared_id, seen_revision FROM shared_follows WHERE library_id=?`, l.id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var rev int
		if err := rows.Scan(&id, &rev); err != nil {
			return nil, err
		}
		if _, ok := shared[id]; ok {
			out[id] = rev
		}
	}
	return out, rows.Err()
}

// SaveAnnotation saves the library's annotation of a shared channel
// (c.Link.SharedID): c's tags, notes, and short names. Clearing them all
// removes the annotation. It returns the library's view of the channel,
// and the library follows it from then on.
func (l *library) SaveAnnotation(ctx context.Context, c domain.Channel) (domain.Channel, error) {
	var view domain.Channel
	err := l.inTx(ctx, func(tx *library) error {
		isShared, err := tx.shared(ctx)
		if err != nil {
			return err
		}
		if isShared {
			return domain.Invalidf("a shared channel can't link to another")
		}
		target, err := tx.sharedChannel(ctx, c.Link.SharedID)
		if err != nil {
			return err
		}
		var rowID string
		err = tx.q.QueryRowContext(ctx, `SELECT id FROM channels WHERE link_id=? AND library_id=?`, target.ID, tx.id).Scan(&rowID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		tags, notes := domain.NormalizeTags(c.Tags), strings.TrimSpace(c.Notes)
		shortNames := domain.NormalizeShortNames(target.Name, c.ShortNames)
		if err := tx.Follow(ctx, target.ID); err != nil {
			return err
		}
		if err := tx.recordNotes(ctx, target.ID, notesRecord{tags, notes, shortNames}); err != nil {
			return err
		}
		if len(tags) == 0 && notes == "" && len(shortNames) == 0 {
			if rowID != "" {
				if _, err := tx.q.ExecContext(ctx, `DELETE FROM channels WHERE id=?`, rowID); err != nil {
					return fmt.Errorf("delete annotation: %w", err)
				}
			}
			views, err := tx.SharedViews(ctx, []string{target.ID})
			view = views[target.ID]
			return err
		}
		// The row keeps a copy of the shared fields, which reads ignore.
		row := target
		row.ID, row.Tags, row.Notes, row.ShortNames, row.Power, row.Skip = rowID, tags, c.Notes, shortNames, domain.PowerUnset, false
		row.Link, row.Orgs, row.Networks, row.Revision, row.CopiedFrom = &domain.Link{SharedID: target.ID}, nil, nil, 0, ""
		row.Status, row.StatusNote, row.BackOn, row.ReplacedBy = domain.StatusActive, "", nil, ""
		var old *domain.Channel
		if rowID == "" {
			if err := freeID(ctx, tx.q, "channels", &row.ID); err != nil {
				return err
			}
			if err := assignIdentity(&row.ID, &row.CreatedAt, &row.UpdatedAt); err != nil {
				return err
			}
		} else {
			row.UpdatedAt = time.Now().UTC()
			old = &row
		}
		if _, err := tx.writeChannel(ctx, row, old); err != nil {
			return err
		}
		views, err := tx.SharedViews(ctx, []string{target.ID})
		view = views[target.ID]
		return err
	})
	return view, err
}

// Follow makes the library follow a shared channel, as seen at its current
// revision, unless it already does.
func (l *library) Follow(ctx context.Context, sharedID string) error {
	_, err := l.q.ExecContext(ctx, `INSERT INTO shared_follows (library_id, shared_id, seen_revision)
		SELECT ?, id, revision FROM channels WHERE id=? AND link_id IS NULL ON CONFLICT DO NOTHING`, l.id, sharedID)
	return err
}

// MarkSeen records the shared channel's revision the library has seen.
func (l *library) MarkSeen(ctx context.Context, sharedID string, revision int) error {
	_, err := l.q.ExecContext(ctx, `INSERT INTO shared_follows (library_id, shared_id, seen_revision) VALUES (?,?,?)
		ON CONFLICT(library_id, shared_id) DO UPDATE SET seen_revision=excluded.seen_revision`, l.id, sharedID, revision)
	return err
}

func scanChannel(r scanner) (domain.Channel, error) {
	var c domain.Channel
	var rtCT, ttCT sql.NullInt64
	var rtD, ttD sql.NullInt64
	var rtInv, ttInv, ts, skip bool
	var created, updated string
	var tags, shortNames, linkID, backOn, replacedBy, networks, orgs sql.NullString
	var lat, lon, radius sql.NullFloat64
	var seen int
	err := r.Scan(&c.ID, &lat, &lon, &radius, &c.Name, &c.ReceiveHz, &c.TransmitHz, &c.Mode, &c.Service, &c.RXOnly, &c.ReceiveTone.Kind, &rtCT, &rtD, &rtInv, &c.TransmitTone.Kind, &ttCT, &ttD, &ttInv, &ts, &c.Power, &c.Bandwidth, &skip, &c.Notes,
		&linkID, &seen, &c.Status, &c.StatusNote, &backOn, &replacedBy, &c.Revision, &c.CopiedFrom, &created, &updated, &tags, &shortNames, &networks, &orgs)
	if err != nil {
		return c, err
	}
	if lat.Valid && lon.Valid {
		c.Location = &domain.Location{Latitude: lat.Float64, Longitude: lon.Float64}
		if radius.Valid {
			c.Location.RadiusKM = &radius.Float64
		}
	}
	if linkID.Valid {
		c.Link = &domain.Link{SharedID: linkID.String, SeenRevision: seen}
	}
	if backOn.Valid {
		if t, err := time.Parse(time.DateOnly, backOn.String); err == nil {
			c.BackOn = &t
		}
	}
	c.ReplacedBy = replacedBy.String
	if rtCT.Valid {
		c.ReceiveTone.CTCSSMilliHz = &rtCT.Int64
	}
	if rtD.Valid {
		x := int(rtD.Int64)
		c.ReceiveTone.DCSCode = &x
	}
	c.ReceiveTone.Inverted = rtInv
	if ttCT.Valid {
		c.TransmitTone.CTCSSMilliHz = &ttCT.Int64
	}
	if ttD.Valid {
		x := int(ttD.Int64)
		c.TransmitTone.DCSCode = &x
	}
	c.TransmitTone.Inverted = ttInv
	c.ToneSquelch = ts
	c.Skip = skip
	c.CreatedAt, c.UpdatedAt = parseTime(created), parseTime(updated)
	if shortNames.Valid && shortNames.String != "" {
		c.ShortNames = strings.Split(shortNames.String, listSeparator)
	}
	if tags.Valid && tags.String != "" {
		c.Tags = strings.Split(tags.String, listSeparator)
	}
	c.Networks, c.Orgs = parseNetworks(networks), parseOrgRefs(orgs)
	return c, nil
}

// --- revisions ---

// revisionRecord is a shared channel as a revision stores it: the fields
// the shared channel owns.
type revisionRecord struct {
	Name         string          `json:"name"`
	ReceiveHz    int64           `json:"receive_hz"`
	TransmitHz   *int64          `json:"transmit_hz,omitempty"`
	Mode         string          `json:"mode"`
	Service      string          `json:"service,omitempty"`
	RXOnly       bool            `json:"rx_only,omitempty"`
	ReceiveTone  string          `json:"receive_tone"`
	TransmitTone string          `json:"transmit_tone"`
	ToneSquelch  bool            `json:"tone_squelch,omitempty"`
	Bandwidth    string          `json:"bandwidth,omitempty"`
	Latitude     *float64        `json:"latitude,omitempty"`
	Longitude    *float64        `json:"longitude,omitempty"`
	RadiusKM     *float64        `json:"radius_km,omitempty"`
	Tags         []string        `json:"tags,omitempty"`
	Notes        string          `json:"notes,omitempty"`
	Status       string          `json:"status"`
	StatusNote   string          `json:"status_note,omitempty"`
	BackOn       string          `json:"back_on,omitempty"`
	ReplacedBy   string          `json:"replaced_by,omitempty"`
	Networks     []string        `json:"networks,omitempty"`
	Orgs         []domain.OrgRef `json:"organizations,omitempty"`
	// A channel of a private library also records what only it has.
	ShortNames []string `json:"short_names,omitempty"`
	Power      string   `json:"power,omitempty"`
	Skip       bool     `json:"skip,omitempty"`
}

// newRecord is a channel's fields as a revision record: everything that
// says what it is and how it's programmed.
func newRecord(c domain.Channel) revisionRecord {
	r := revisionRecord{Name: c.Name, ReceiveHz: c.ReceiveHz, TransmitHz: c.TransmitHz, Mode: c.Mode, Service: string(c.Service), RXOnly: c.RXOnly,
		ReceiveTone: domain.FormatTone(c.ReceiveTone), TransmitTone: domain.FormatTone(c.TransmitTone), ToneSquelch: c.ToneSquelch, Bandwidth: c.Bandwidth,
		Tags: c.Tags, Notes: c.Notes, Status: string(c.Status), StatusNote: c.StatusNote, ReplacedBy: c.ReplacedBy, Orgs: c.Orgs,
		ShortNames: c.ShortNames, Power: string(c.Power), Skip: c.Skip}
	for _, n := range c.Networks {
		r.Networks = append(r.Networks, n.String())
	}
	if l := c.Location; l != nil {
		r.Latitude, r.Longitude, r.RadiusKM = &l.Latitude, &l.Longitude, l.RadiusKM
	}
	if c.BackOn != nil {
		r.BackOn = c.BackOn.Format(time.DateOnly)
	}
	return r
}

// channel is a revision record's fields as a channel with the given ID.
func (r revisionRecord) channel(id string) domain.Channel {
	ch := domain.Channel{ID: id, Name: r.Name, ReceiveHz: r.ReceiveHz, TransmitHz: r.TransmitHz, Mode: r.Mode, Service: domain.Service(r.Service), RXOnly: r.RXOnly,
		ToneSquelch: r.ToneSquelch, Bandwidth: r.Bandwidth, Tags: r.Tags, Notes: r.Notes, Status: domain.Status(r.Status), StatusNote: r.StatusNote, ReplacedBy: r.ReplacedBy,
		ShortNames: r.ShortNames, Power: domain.PowerIntent(r.Power), Skip: r.Skip}
	ch.ReceiveTone, _ = domain.ParseTone(r.ReceiveTone)
	ch.TransmitTone, _ = domain.ParseTone(r.TransmitTone)
	if r.Latitude != nil && r.Longitude != nil {
		ch.Location = &domain.Location{Latitude: *r.Latitude, Longitude: *r.Longitude, RadiusKM: r.RadiusKM}
	}
	if r.BackOn != "" {
		if t, err := time.Parse(time.DateOnly, r.BackOn); err == nil {
			ch.BackOn = &t
		}
	}
	ch.Orgs = r.Orgs
	for _, v := range r.Networks {
		if n, err := domain.ParseNetworkID(v); err == nil {
			ch.Networks = append(ch.Networks, n)
		}
	}
	return ch
}

func (tx *library) recordRevision(ctx context.Context, c domain.Channel) error {
	data, err := json.Marshal(newRecord(c))
	if err != nil {
		return err
	}
	reason, author := store.ChangeOf(ctx)
	_, err = tx.q.ExecContext(ctx, `INSERT INTO channel_revisions (channel_id,revision,changed_at,reason,author,record) VALUES (?,?,?,?,?,?)`,
		c.ID, c.Revision, timestamp(time.Now().UTC()), reason, author, string(data))
	return err
}

func (l *library) ChannelRevisions(ctx context.Context, id string) ([]domain.Revision, error) {
	c, err := l.GetChannel(ctx, id)
	if err != nil {
		return nil, err
	}
	if c.Link != nil {
		id = c.Link.SharedID
	}
	rows, err := l.q.QueryContext(ctx, `SELECT revision,changed_at,reason,author,record FROM channel_revisions WHERE channel_id=? ORDER BY revision`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Revision
	for rows.Next() {
		var rev domain.Revision
		var at, data string
		if err := rows.Scan(&rev.Number, &at, &rev.Reason, &rev.Author, &data); err != nil {
			return nil, err
		}
		rev.At = parseTime(at)
		var r revisionRecord
		if err := json.Unmarshal([]byte(data), &r); err != nil {
			return nil, fmt.Errorf("revision %d: %w", rev.Number, err)
		}
		ch := r.channel(id)
		ch.Revision = rev.Number
		rev.Channel = ch
		out = append(out, rev)
	}
	return out, rows.Err()
}

func (l *library) ChangedSince(ctx context.Context, since time.Time) ([]domain.Channel, time.Time, error) {
	// Revision times are compared parsed: RFC 3339 text with trimmed
	// fractions doesn't sort.
	rows, err := l.q.QueryContext(ctx, `SELECT r.channel_id, r.changed_at FROM channel_revisions r JOIN channels c ON c.id=r.channel_id WHERE c.library_id=?`, l.id)
	if err != nil {
		return nil, since, err
	}
	latest := since
	var ids []any
	seen := map[string]bool{}
	for rows.Next() {
		var id, at string
		if err := rows.Scan(&id, &at); err != nil {
			_ = rows.Close()
			return nil, since, err
		}
		if t := parseTime(at); t.After(since) {
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
			if t.After(latest) {
				latest = t
			}
		}
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil || len(ids) == 0 {
		return nil, since, err
	}
	cs, err := l.listChannels(ctx, `c.library_id=? AND c.id IN (`+strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")+`)`, append([]any{l.id}, ids...)...)
	return cs, latest, err
}

// --- built-in list records ---

func (l *library) SeedChannels(ctx context.Context) ([]domain.SeedChannel, error) {
	rows, err := l.q.QueryContext(ctx, `SELECT s.channel_id, s.list, s.name, s.applied FROM seed_channels s JOIN channels c ON c.id=s.channel_id WHERE c.library_id=? ORDER BY s.list, s.name`, l.id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.SeedChannel
	for rows.Next() {
		var sc domain.SeedChannel
		var data string
		if err := rows.Scan(&sc.ChannelID, &sc.List, &sc.Name, &data); err != nil {
			return nil, err
		}
		var r revisionRecord
		if err := json.Unmarshal([]byte(data), &r); err != nil {
			return nil, fmt.Errorf("built-in list record for %s: %w", sc.Name, err)
		}
		sc.Applied = r.channel(sc.ChannelID)
		out = append(out, sc)
	}
	return out, rows.Err()
}

func (l *library) SetSeedChannel(ctx context.Context, sc domain.SeedChannel) error {
	if _, err := l.GetChannel(ctx, sc.ChannelID); err != nil {
		return err
	}
	data, err := json.Marshal(newRecord(sc.Applied))
	if err != nil {
		return err
	}
	_, err = l.q.ExecContext(ctx, `INSERT INTO seed_channels (channel_id,list,name,applied) VALUES (?,?,?,?)
		ON CONFLICT(channel_id) DO UPDATE SET list=excluded.list, name=excluded.name, applied=excluded.applied`, sc.ChannelID, sc.List, sc.Name, string(data))
	return err
}

// sameRecord reports whether two channels record the same revision.
func sameRecord(a, b domain.Channel) bool {
	x, err1 := json.Marshal(newRecord(a))
	y, err2 := json.Marshal(newRecord(b))
	return err1 == nil && err2 == nil && string(x) == string(y)
}

// notesRecord is a revision of the user's own notes on a shared channel:
// tags, notes, and short names.
type notesRecord struct {
	Tags       []string `json:"tags,omitempty"`
	Notes      string   `json:"notes,omitempty"`
	ShortNames []string `json:"short_names,omitempty"`
}

// recordNotes records a revision of the user's notes on a shared channel
// when they differ from the last one (none being empty notes). The history
// is kept by shared channel, so it outlasts the annotation row, which goes
// when the notes are emptied.
func (l *library) recordNotes(ctx context.Context, sharedID string, rec notesRecord) error {
	data, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	last, lastData := 0, "{}"
	err = l.q.QueryRowContext(ctx, `SELECT revision, record FROM annotation_revisions WHERE library_id=? AND shared_id=? ORDER BY revision DESC LIMIT 1`, l.id, sharedID).Scan(&last, &lastData)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if string(data) == lastData {
		return nil
	}
	reason, author := store.ChangeOf(ctx)
	_, err = l.q.ExecContext(ctx, `INSERT INTO annotation_revisions (library_id,shared_id,revision,changed_at,reason,author,record) VALUES (?,?,?,?,?,?,?)`,
		l.id, sharedID, last+1, timestamp(time.Now().UTC()), reason, author, string(data))
	return err
}

func (l *library) NotesRevision(ctx context.Context, sharedID string) (int, error) {
	var n int
	err := l.q.QueryRowContext(ctx, `SELECT coalesce(max(revision),0) FROM annotation_revisions WHERE library_id=? AND shared_id=?`, l.id, sharedID).Scan(&n)
	return n, err
}
