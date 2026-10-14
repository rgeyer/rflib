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
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(&library{db: l.db, q: tx, tx: tx, id: l.id}); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
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
