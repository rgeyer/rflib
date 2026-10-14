package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ryan/rflib/internal/domain"
	"github.com/ryan/rflib/internal/store"
)

const searchColumns = `id,name,query,notes,created_at,updated_at`

// CreateSearch saves a named search. The caller checks that the query
// compiles; the store only checks the name.
func (l *library) CreateSearch(ctx context.Context, x domain.SavedSearch) (domain.SavedSearch, error) {
	x.Name = strings.ToLower(strings.TrimSpace(x.Name))
	if err := domain.ValidateSavedSearch(x); err != nil {
		return x, err
	}
	if err := assignIdentity(&x.ID, &x.CreatedAt, &x.UpdatedAt); err != nil {
		return x, err
	}
	_, err := l.q.ExecContext(ctx, `INSERT INTO searches (library_id,`+searchColumns+`) VALUES (?,?,?,?,?,?,?)`,
		l.id, x.ID, x.Name, x.Query, x.Notes, timestamp(x.CreatedAt), timestamp(x.UpdatedAt))
	if err != nil {
		return x, conflict(fmt.Errorf("save search: %w", err), "a saved search named %q", x.Name)
	}
	return x, nil
}

// GetSearch looks a saved search up by ID or name; a leading @ is ignored.
func (l *library) GetSearch(ctx context.Context, idOrName string) (domain.SavedSearch, error) {
	ref := strings.TrimPrefix(strings.TrimSpace(idOrName), "@")
	x, err := scanSearch(l.q.QueryRowContext(ctx, `SELECT `+searchColumns+` FROM searches WHERE library_id=? AND (id=? OR name=?)`, l.id, ref, strings.ToLower(ref)))
	if errors.Is(err, sql.ErrNoRows) {
		return x, fmt.Errorf("no saved search named %q: %w", ref, store.ErrNotFound)
	}
	return x, err
}

func (l *library) ListSearches(ctx context.Context) ([]domain.SavedSearch, error) {
	rows, err := l.q.QueryContext(ctx, `SELECT `+searchColumns+` FROM searches WHERE library_id=? ORDER BY name`, l.id)
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

func (l *library) UpdateSearch(ctx context.Context, x domain.SavedSearch) (domain.SavedSearch, error) {
	x.Name = strings.ToLower(strings.TrimSpace(x.Name))
	if err := domain.ValidateSavedSearch(x); err != nil {
		return x, err
	}
	x.UpdatedAt = time.Now().UTC()
	res, err := l.q.ExecContext(ctx, `UPDATE searches SET name=?,query=?,notes=?,updated_at=? WHERE id=? AND library_id=?`,
		x.Name, x.Query, x.Notes, timestamp(x.UpdatedAt), x.ID, l.id)
	if err != nil {
		return x, conflict(fmt.Errorf("update search: %w", err), "a saved search named %q", x.Name)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return x, fmt.Errorf("saved search %q: %w", x.ID, store.ErrNotFound)
	}
	return x, nil
}

func (l *library) DeleteSearch(ctx context.Context, id string) error {
	res, err := l.q.ExecContext(ctx, `DELETE FROM searches WHERE id=? AND library_id=?`, id, l.id)
	if err != nil {
		return fmt.Errorf("delete search: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("saved search %q: %w", id, store.ErrNotFound)
	}
	return nil
}

func scanSearch(r scanner) (domain.SavedSearch, error) {
	var x domain.SavedSearch
	var created, updated string
	if err := r.Scan(&x.ID, &x.Name, &x.Query, &x.Notes, &created, &updated); err != nil {
		return x, err
	}
	x.CreatedAt, x.UpdatedAt = parseTime(created), parseTime(updated)
	return x, nil
}

func (l *library) RadioChannelIDs(ctx context.Context, radioID string) (map[string]bool, error) {
	q, args := `SELECT rc.channel_id FROM radio_channels rc JOIN radios r ON r.id=rc.radio_id WHERE r.library_id=?`, []any{l.id}
	if radioID != "" {
		if err := l.ownsRadio(ctx, radioID); err != nil {
			return nil, err
		}
		q, args = q+` AND r.id=?`, append(args, radioID)
	}
	rows, err := l.q.QueryContext(ctx, q, args...)
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
