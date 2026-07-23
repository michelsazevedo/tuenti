package database

import (
	"context"

	"github.com/jackc/pgx/v5"
)

type UnitOfWork struct {
	db *PgConn
}

func NewUnitOfWork(db *PgConn) *UnitOfWork {
	return &UnitOfWork{db: db}
}

func (u *UnitOfWork) Do(ctx context.Context, fn func(tx pgx.Tx) error) error {
	tx, err := u.db.Begin(ctx)
	if err != nil {
		return err
	}

	defer tx.Rollback(ctx)

	if err := fn(tx); err != nil {
		return err
	}

	return tx.Commit(ctx)
}
