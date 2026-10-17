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

// localMethod is the identity method of a local password.
const localMethod = "local"

const userColumns = `u.id,u.name,u.display_name,u.email,u.role,u.disabled,u.created_at,coalesce((SELECT id FROM libraries WHERE owner_id=u.id),'')`

func scanUser(r scanner) (domain.User, error) {
	var u domain.User
	var role, created string
	if err := r.Scan(&u.ID, &u.Name, &u.DisplayName, &u.Email, &role, &u.Disabled, &created, &u.LibraryID); err != nil {
		return u, err
	}
	u.Role, u.CreatedAt = domain.Role(role), parseTime(created)
	return u, nil
}

func (d *DB) CreateUser(ctx context.Context, u domain.User, passwordHash string, opts store.NewUser) (domain.User, error) {
	if err := domain.ValidateUser(u); err != nil {
		return u, err
	}
	var err error
	if u.ID, err = domain.NewID(); err != nil {
		return u, err
	}
	u.CreatedAt = time.Now().UTC()
	err = d.inTx(ctx, func(tx *sql.Tx) error {
		if opts.Invite != "" {
			if err := checkInvite(ctx, tx, opts.Invite); err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO users (id,name,display_name,email,role,disabled,created_at) VALUES (?,?,?,?,?,?,?)`,
			u.ID, u.Name, u.DisplayName, u.Email, string(u.Role), u.Disabled, timestamp(u.CreatedAt))
		if err = conflict(err, "the name %q is taken", u.Name); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO identities (method,subject,user_id,secret,created_at) VALUES (?,?,?,?,?)`,
			localMethod, u.ID, u.ID, passwordHash, timestamp(u.CreatedAt)); err != nil {
			return err
		}
		if opts.Claim != "" {
			res, err := tx.ExecContext(ctx, `UPDATE libraries SET owner_id=? WHERE (id=? OR name=?) AND kind<>? AND owner_id IS NULL`, u.ID, opts.Claim, opts.Claim, sharedKind)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n == 0 {
				return fmt.Errorf("no library %q without an owner: %w", opts.Claim, store.ErrNotFound)
			}
		} else {
			lib := domain.Library{Name: strings.ToLower(u.Name)}
			if err := assignIdentity(&lib.ID, &lib.CreatedAt, &lib.UpdatedAt); err != nil {
				return err
			}
			_, err := tx.ExecContext(ctx, `INSERT INTO libraries (id,name,description,kind,is_default,created_at,updated_at,owner_id) VALUES (?,?,'','',0,?,?,?)`,
				lib.ID, lib.Name, timestamp(lib.CreatedAt), timestamp(lib.UpdatedAt), u.ID)
			if err = conflict(err, "the name %q is taken", u.Name); err != nil {
				return err
			}
		}
		if opts.Invite != "" {
			if _, err := tx.ExecContext(ctx, `UPDATE invites SET used_by=?, used_at=? WHERE code_hash=?`, u.ID, timestamp(time.Now().UTC()), opts.Invite); err != nil {
				return err
			}
		}
		return tx.QueryRowContext(ctx, `SELECT id FROM libraries WHERE owner_id=?`, u.ID).Scan(&u.LibraryID)
	})
	if err != nil {
		return u, fmt.Errorf("create user: %w", err)
	}
	return u, nil
}

// checkInvite refuses an invite that's unknown, used, or expired.
func checkInvite(ctx context.Context, tx *sql.Tx, codeHash string) error {
	var expires string
	var used sql.NullString
	err := tx.QueryRowContext(ctx, `SELECT expires_at, used_at FROM invites WHERE code_hash=?`, codeHash).Scan(&expires, &used)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && (used.Valid || !time.Now().Before(parseTime(expires)))) {
		return fmt.Errorf("that invitation code isn't valid, or was used already: %w", store.ErrNotFound)
	}
	return err
}

func (d *DB) GetUser(ctx context.Context, idOrName string) (domain.User, error) {
	u, err := scanUser(d.q().QueryRowContext(ctx, `SELECT `+userColumns+` FROM users u WHERE u.id=? OR u.name=? COLLATE NOCASE`, idOrName, idOrName))
	if errors.Is(err, sql.ErrNoRows) {
		return u, fmt.Errorf("user %q: %w", idOrName, store.ErrNotFound)
	}
	return u, err
}

func (d *DB) ListUsers(ctx context.Context) ([]domain.User, error) {
	rows, err := d.q().QueryContext(ctx, `SELECT `+userColumns+` FROM users u ORDER BY u.name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (d *DB) UpdateUser(ctx context.Context, u domain.User) (domain.User, error) {
	if err := domain.ValidateUser(u); err != nil {
		return u, err
	}
	res, err := d.q().ExecContext(ctx, `UPDATE users SET display_name=?, email=?, role=?, disabled=? WHERE id=?`,
		u.DisplayName, u.Email, string(u.Role), u.Disabled, u.ID)
	if err != nil {
		return u, fmt.Errorf("update user: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return u, fmt.Errorf("user %q: %w", u.ID, store.ErrNotFound)
	}
	return d.GetUser(ctx, u.ID)
}

func (d *DB) DeleteUser(ctx context.Context, id string) error {
	return d.inTx(ctx, func(tx *sql.Tx) error {
		var isDefault bool
		err := tx.QueryRowContext(ctx, `SELECT is_default FROM libraries WHERE owner_id=?`, id).Scan(&isDefault)
		if err == nil && isDefault {
			return fmt.Errorf("the owner of the default library can't be removed: %w", store.ErrConflict)
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM libraries WHERE owner_id=?`, id); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `DELETE FROM users WHERE id=?`, id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return fmt.Errorf("user %q: %w", id, store.ErrNotFound)
		}
		return nil
	})
}

func (d *DB) PasswordHash(ctx context.Context, userID string) (string, error) {
	var h string
	err := d.q().QueryRowContext(ctx, `SELECT secret FROM identities WHERE method=? AND subject=?`, localMethod, userID).Scan(&h)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("no password for user %q: %w", userID, store.ErrNotFound)
	}
	return h, err
}

func (d *DB) SetPasswordHash(ctx context.Context, userID, hash string) error {
	_, err := d.q().ExecContext(ctx, `INSERT INTO identities (method,subject,user_id,secret,created_at) VALUES (?,?,?,?,?)
		ON CONFLICT(method, subject) DO UPDATE SET secret=excluded.secret`, localMethod, userID, userID, hash, timestamp(time.Now().UTC()))
	if err != nil && strings.Contains(err.Error(), "FOREIGN KEY") {
		return fmt.Errorf("user %q: %w", userID, store.ErrNotFound)
	}
	return err
}

func (d *DB) CreateSession(ctx context.Context, s domain.Session) error {
	_, err := d.q().ExecContext(ctx, `INSERT INTO sessions (token_hash,user_id,created_at,expires_at) VALUES (?,?,?,?)`,
		s.TokenHash, s.UserID, timestamp(s.CreatedAt), timestamp(s.ExpiresAt))
	return err
}

func (d *DB) Session(ctx context.Context, tokenHash string) (domain.Session, error) {
	s := domain.Session{TokenHash: tokenHash}
	var created, expires string
	err := d.q().QueryRowContext(ctx, `SELECT user_id, created_at, expires_at FROM sessions WHERE token_hash=?`, tokenHash).Scan(&s.UserID, &created, &expires)
	if err == nil {
		s.CreatedAt, s.ExpiresAt = parseTime(created), parseTime(expires)
		if !time.Now().Before(s.ExpiresAt) {
			_ = d.DeleteSession(ctx, tokenHash)
			err = sql.ErrNoRows
		}
	}
	if errors.Is(err, sql.ErrNoRows) {
		return s, fmt.Errorf("session: %w", store.ErrNotFound)
	}
	return s, err
}

func (d *DB) ExtendSession(ctx context.Context, tokenHash string, expires time.Time) error {
	_, err := d.q().ExecContext(ctx, `UPDATE sessions SET expires_at=? WHERE token_hash=?`, timestamp(expires), tokenHash)
	return err
}

func (d *DB) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := d.q().ExecContext(ctx, `DELETE FROM sessions WHERE token_hash=?`, tokenHash)
	return err
}

func (d *DB) DeleteSessions(ctx context.Context, userID, keep string) error {
	_, err := d.q().ExecContext(ctx, `DELETE FROM sessions WHERE user_id=? AND token_hash<>?`, userID, keep)
	return err
}

func (d *DB) CreateInvite(ctx context.Context, inv domain.Invite) error {
	_, err := d.q().ExecContext(ctx, `INSERT INTO invites (code_hash,created_by,created_at,expires_at) VALUES (?,?,?,?)`,
		inv.CodeHash, inv.CreatedBy, timestamp(inv.CreatedAt), timestamp(inv.ExpiresAt))
	return err
}

func (d *DB) ListInvites(ctx context.Context) ([]domain.Invite, error) {
	rows, err := d.q().QueryContext(ctx, `SELECT code_hash, created_by, created_at, expires_at, coalesce(used_by,''), used_at FROM invites ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Invite
	for rows.Next() {
		var inv domain.Invite
		var created, expires string
		var used sql.NullString
		if err := rows.Scan(&inv.CodeHash, &inv.CreatedBy, &created, &expires, &inv.UsedBy, &used); err != nil {
			return nil, err
		}
		inv.CreatedAt, inv.ExpiresAt = parseTime(created), parseTime(expires)
		if used.Valid {
			t := parseTime(used.String)
			inv.UsedAt = &t
		}
		out = append(out, inv)
	}
	return out, rows.Err()
}
