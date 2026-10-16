package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/ryan/rflib/internal/store"
)

// library is one library's data. Every statement is confined to its ID. A
// library bound to a transaction (tx set) runs everything in it.
type library struct {
	db *sql.DB
	q  querier
	tx *sql.Tx
	id string
}

var _ store.Library = (*library)(nil)

func (l *library) ID() string { return l.id }

func (l *library) InTx(ctx context.Context, fn func(store.Library) error) error {
	return l.inTx(ctx, func(tx *library) error { return fn(tx) })
}

func (l *library) inTx(ctx context.Context, fn func(*library) error) error {
	if l.tx != nil {
		return fn(l)
	}
	// Once begun, a write transaction always finishes: its statements
	// ignore a canceled request, which would otherwise interrupt them on the
	// database's one connection and could leave it inside the transaction.
	tx, err := l.db.BeginTx(context.WithoutCancel(ctx), nil)
	if err != nil {
		return err
	}
	if err := fn(&library{db: l.db, q: uncanceled{tx}, tx: tx, id: l.id}); err != nil {
		_ = tx.Rollback()
		return err
	}
	return commit(ctx, l.db, tx)
}

// uncanceledDB runs a library's reads whether or not their context is
// canceled (see DB.Library).
type uncanceledDB struct{ db *sql.DB }

func (u uncanceledDB) ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error) {
	return u.db.ExecContext(context.WithoutCancel(ctx), q, args...)
}

func (u uncanceledDB) QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	return u.db.QueryContext(context.WithoutCancel(ctx), q, args...)
}

func (u uncanceledDB) QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row {
	return u.db.QueryRowContext(context.WithoutCancel(ctx), q, args...)
}

// uncanceled runs a transaction's statements whether or not their context
// is canceled (see inTx).
type uncanceled struct{ tx *sql.Tx }

func (u uncanceled) ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error) {
	return u.tx.ExecContext(context.WithoutCancel(ctx), q, args...)
}

func (u uncanceled) QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	return u.tx.QueryContext(context.WithoutCancel(ctx), q, args...)
}

func (u uncanceled) QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row {
	return u.tx.QueryRowContext(context.WithoutCancel(ctx), q, args...)
}

// owns checks that a record in table belongs to this library.
func (l *library) owns(ctx context.Context, table, what, id string) error {
	var n int
	if err := l.q.QueryRowContext(ctx, `SELECT count(*) FROM `+table+` WHERE id=? AND library_id=?`, id, l.id).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("%s %q: %w", what, id, store.ErrNotFound)
	}
	return nil
}
