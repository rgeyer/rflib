package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ryan/rflib/internal/domain"
	"github.com/ryan/rflib/internal/store"
)

func (l *library) RadioSnapshot(ctx context.Context, radioID string) (domain.RadioSnapshot, error) {
	r, err := l.GetRadio(ctx, radioID)
	if err != nil {
		return domain.RadioSnapshot{}, err
	}
	snap := domain.RadioSnapshot{Name: r.Name, Model: r.Model, DefaultPower: r.DefaultPower, Dedupe: r.Dedupe, NameLength: r.NameLength, Notes: r.Notes,
		Sections: []domain.SnapshotSection{}, Sources: []domain.SnapshotSource{}, Memories: []domain.SnapshotMemory{}}
	secs, err := listSections(ctx, l.q, r.ID)
	if err != nil {
		return snap, err
	}
	for _, s := range secs {
		snap.Sections = append(snap.Sections, domain.SnapshotSection{ID: s.ID, Name: s.Name, Start: s.Start, Size: s.Size, Sort: s.Sort, Default: s.Default})
	}
	srcs, err := listRadioSources(ctx, l.q, r.ID)
	if err != nil {
		return snap, err
	}
	for _, s := range srcs {
		snap.Sources = append(snap.Sources, domain.SnapshotSource{ID: s.ID, SectionID: s.SectionID, Query: s.Query, PlanID: s.PlanID, Sort: s.Sort, PlanNumbers: s.PlanNumbers})
	}
	es, err := l.RadioEntries(ctx, r.ID)
	if err != nil {
		return snap, err
	}
	for _, e := range es {
		m := domain.SnapshotMemory{ChannelID: e.Channel.ID, ChannelRevision: e.Channel.Revision, SectionID: e.Section.ID,
			Name: e.Name, Power: e.Power, Skip: e.Skip, SourceID: e.SourceID, PlanNumber: e.PlanNumber}
		if e.Channel.Link != nil {
			if m.NotesRevision, err = l.NotesRevision(ctx, e.Channel.Link.SharedID); err != nil {
				return snap, err
			}
		}
		snap.Memories = append(snap.Memories, m)
	}
	return snap, nil
}

func (l *library) SaveRadioRevision(ctx context.Context, radioID string, snap domain.RadioSnapshot, exported bool) (domain.RadioRevision, error) {
	rev := domain.RadioRevision{At: time.Now().UTC(), Exported: exported, Snapshot: snap}
	rev.Reason, rev.Author = store.ChangeOf(ctx)
	data, err := json.Marshal(snap)
	if err != nil {
		return rev, err
	}
	err = l.inTx(ctx, func(tx *library) error {
		if err := tx.ownsRadio(ctx, radioID); err != nil {
			return err
		}
		if err := tx.q.QueryRowContext(ctx, `SELECT coalesce(max(revision),0)+1 FROM radio_revisions WHERE radio_id=?`, radioID).Scan(&rev.Number); err != nil {
			return err
		}
		_, err := tx.q.ExecContext(ctx, `INSERT INTO radio_revisions (radio_id,revision,saved_at,reason,author,exported,record) VALUES (?,?,?,?,?,?,?)`,
			radioID, rev.Number, timestamp(rev.At), rev.Reason, rev.Author, exported, string(data))
		return err
	})
	return rev, err
}

func (l *library) RadioRevisions(ctx context.Context, radioID string) ([]domain.RadioRevision, error) {
	if err := l.ownsRadio(ctx, radioID); err != nil {
		return nil, err
	}
	rows, err := l.q.QueryContext(ctx, `SELECT revision,saved_at,reason,author,exported,record FROM radio_revisions WHERE radio_id=? ORDER BY revision`, radioID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.RadioRevision
	for rows.Next() {
		var rev domain.RadioRevision
		var at, data string
		if err := rows.Scan(&rev.Number, &at, &rev.Reason, &rev.Author, &rev.Exported, &data); err != nil {
			return nil, err
		}
		rev.At = parseTime(at)
		if err := json.Unmarshal([]byte(data), &rev.Snapshot); err != nil {
			return nil, fmt.Errorf("radio revision %d: %w", rev.Number, err)
		}
		out = append(out, rev)
	}
	return out, rows.Err()
}

func (l *library) MarkRadioRevisionExported(ctx context.Context, radioID string, revision int) error {
	if err := l.ownsRadio(ctx, radioID); err != nil {
		return err
	}
	res, err := l.q.ExecContext(ctx, `UPDATE radio_revisions SET exported=1 WHERE radio_id=? AND revision=?`, radioID, revision)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("revision %d: %w", revision, store.ErrNotFound)
	}
	return nil
}

func (l *library) ChannelAt(ctx context.Context, id string, revision int) (domain.Channel, error) {
	var data string
	err := l.q.QueryRowContext(ctx, `SELECT record FROM channel_revisions WHERE channel_id=? AND revision=?`, id, revision).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Channel{}, fmt.Errorf("channel %q revision %d: %w", id, revision, store.ErrNotFound)
	}
	if err != nil {
		return domain.Channel{}, err
	}
	var r revisionRecord
	if err := json.Unmarshal([]byte(data), &r); err != nil {
		return domain.Channel{}, err
	}
	c := r.channel(id)
	c.Revision = revision
	return c, nil
}

func (l *library) NotesAt(ctx context.Context, sharedID string, revision int) (tags []string, notes string, shortNames []string, err error) {
	if revision == 0 {
		return nil, "", nil, nil
	}
	var data string
	err = l.q.QueryRowContext(ctx, `SELECT record FROM annotation_revisions WHERE library_id=? AND shared_id=? AND revision=?`, l.id, sharedID, revision).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", nil, fmt.Errorf("notes on %q revision %d: %w", sharedID, revision, store.ErrNotFound)
	}
	if err != nil {
		return nil, "", nil, err
	}
	var r notesRecord
	err = json.Unmarshal([]byte(data), &r)
	return r.Tags, r.Notes, r.ShortNames, err
}

func (l *library) ReplaceRadioList(ctx context.Context, radioID string, snap domain.RadioSnapshot) error {
	return l.inTx(ctx, func(tx *library) error {
		if err := tx.ownsRadio(ctx, radioID); err != nil {
			return err
		}
		for _, table := range []string{"radio_channels", "radio_sources", "radio_sections"} {
			if _, err := tx.q.ExecContext(ctx, `DELETE FROM `+table+` WHERE radio_id=?`, radioID); err != nil {
				return err
			}
		}
		// New rows, each remembering the one it restores, whose ID it gets
		// back when a draft is saved.
		ids := map[string]string{}
		newID := func(old string) (string, error) {
			var id string
			if err := assignIdentity(&id, new(time.Time), new(time.Time)); err != nil {
				return "", err
			}
			ids[old] = id
			return id, nil
		}
		for i, s := range snap.Sections {
			id, err := newID(s.ID)
			if err != nil {
				return err
			}
			if _, err := tx.q.ExecContext(ctx, `INSERT INTO radio_sections (`+sectionColumns+`,origin) VALUES (?,?,?,?,?,?,?,?,?)`,
				id, radioID, i, s.Name, intOrNil(s.Start), intOrNil(s.Size), s.Sort, s.Default, s.ID); err != nil {
				return fmt.Errorf("restore section %s: %w", s.Name, err)
			}
		}
		for i, s := range snap.Sources {
			id, err := newID(s.ID)
			if err != nil {
				return err
			}
			if _, err := tx.q.ExecContext(ctx, `INSERT INTO radio_sources (`+sourceColumns+`,origin) VALUES (?,?,?,?,?,?,?,?,?)`,
				id, radioID, i, s.Query, s.Sort, nullable(s.PlanID), nullable(ids[s.SectionID]), s.PlanNumbers, s.ID); err != nil {
				return fmt.Errorf("restore source %d: %w", i+1, err)
			}
		}
		for i, m := range snap.Memories {
			if err := tx.canHold(ctx, m.ChannelID); err != nil {
				return err
			}
			var skip any
			if m.Skip != nil {
				skip = *m.Skip
			}
			if _, err := tx.q.ExecContext(ctx, `INSERT INTO radio_channels (radio_id,channel_id,position,name,power,skip,source_id,section_id,plan_number) VALUES (?,?,?,?,?,?,?,?,?)`,
				radioID, m.ChannelID, i, m.Name, string(m.Power), skip, nullable(ids[m.SourceID]), nullable(ids[m.SectionID]), intOrNil(m.PlanNumber)); err != nil {
				return fmt.Errorf("restore memory %d: %w", i+1, err)
			}
		}
		return nil
	})
}
