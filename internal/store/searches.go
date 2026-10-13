package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ryan/rflib/internal/domain"
)

const searchColumns = `id,name,query,notes,created_at,updated_at`

// CreateSearch saves a named search. The caller checks that the query
// compiles; the store only checks the name.
func (s *Store) CreateSearch(ctx context.Context, x domain.SavedSearch) (domain.SavedSearch, error) {
	x.Name = strings.ToLower(strings.TrimSpace(x.Name))
	if err := domain.ValidateSavedSearch(x); err != nil {
		return x, err
	}
	if err := assignIdentity(&x.ID, &x.CreatedAt, &x.UpdatedAt); err != nil {
		return x, err
	}
	_, err := s.q.ExecContext(ctx, `INSERT INTO searches (`+searchColumns+`) VALUES (?,?,?,?,?,?)`,
		x.ID, x.Name, x.Query, x.Notes, x.CreatedAt.Format(time.RFC3339Nano), x.UpdatedAt.Format(time.RFC3339Nano))
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return x, fmt.Errorf("a saved search named %q already exists", x.Name)
		}
		return x, fmt.Errorf("save search: %w", err)
	}
	return x, nil
}

// GetSearch looks a saved search up by ID or name; a leading @ is ignored.
func (s *Store) GetSearch(ctx context.Context, idOrName string) (domain.SavedSearch, error) {
	ref := strings.TrimPrefix(strings.TrimSpace(idOrName), "@")
	x, err := scanSearch(s.q.QueryRowContext(ctx, `SELECT `+searchColumns+` FROM searches WHERE id=? OR name=?`, ref, strings.ToLower(ref)))
	if errors.Is(err, sql.ErrNoRows) {
		return x, fmt.Errorf("no saved search named %q: %w", ref, ErrNotFound)
	}
	return x, err
}

func (s *Store) ListSearches(ctx context.Context) ([]domain.SavedSearch, error) {
	rows, err := s.q.QueryContext(ctx, `SELECT `+searchColumns+` FROM searches ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.SavedSearch
	for rows.Next() {
		x, err := scanSearch(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

func (s *Store) UpdateSearch(ctx context.Context, x domain.SavedSearch) (domain.SavedSearch, error) {
	x.Name = strings.ToLower(strings.TrimSpace(x.Name))
	if err := domain.ValidateSavedSearch(x); err != nil {
		return x, err
	}
	x.UpdatedAt = time.Now().UTC()
	res, err := s.q.ExecContext(ctx, `UPDATE searches SET name=?,query=?,notes=?,updated_at=? WHERE id=?`,
		x.Name, x.Query, x.Notes, x.UpdatedAt.Format(time.RFC3339Nano), x.ID)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return x, fmt.Errorf("a saved search named %q already exists", x.Name)
		}
		return x, fmt.Errorf("update search: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return x, fmt.Errorf("saved search %q: %w", x.ID, ErrNotFound)
	}
	return x, nil
}

func (s *Store) DeleteSearch(ctx context.Context, id string) error {
	res, err := s.q.ExecContext(ctx, `DELETE FROM searches WHERE id=?`, id)
	if err != nil {
		return fmt.Errorf("delete search: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("saved search %q: %w", id, ErrNotFound)
	}
	return nil
}

func scanSearch(r scanner) (domain.SavedSearch, error) {
	var x domain.SavedSearch
	var created, updated string
	if err := r.Scan(&x.ID, &x.Name, &x.Query, &x.Notes, &created, &updated); err != nil {
		return x, err
	}
	x.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	x.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return x, nil
}

// RadioChannelIDs returns the IDs of the channels on a radio (by ID or
// name), or on any radio when ref is empty.
func (s *Store) RadioChannelIDs(ctx context.Context, ref string) (map[string]bool, error) {
	q, args := `SELECT channel_id FROM radio_channels`, []any{}
	if ref != "" {
		r, err := s.GetRadio(ctx, ref)
		if err != nil {
			return nil, err
		}
		q, args = q+` WHERE radio_id=?`, append(args, r.ID)
	}
	rows, err := s.q.QueryContext(ctx, q, args...)
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
