package datastore

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// Tx is the surface handed to a ProducerFunc/TransactionFunc closure: every
// statement pool, conn, and tx share (Querier), never transaction control --
// the closure runs inside a transaction it does not own.
type Tx interface {
	Querier

	// Raw returns the underlying pgx.Tx, for anything outside this
	// interface's surface (LargeObjects, Prepare, a nested Begin).
	// Escape hatch, not the default path.
	Raw() pgx.Tx
}

type TransactionFunc func(ctx context.Context, tx Tx) error

type vulkanTx struct {
	pgx.Tx
}

func newTx(tx pgx.Tx) *vulkanTx {
	return &vulkanTx{tx}
}

func (t *vulkanTx) Raw() pgx.Tx {
	return t.Tx
}

// InTransaction opens one transaction, runs transactionFunc against it, and
// commits. No retry: only the caller knows what's safe to rerun in its
// closure.
func InTransaction(ctx context.Context, ds *PostgresDatastore, transactionFunc TransactionFunc) error {
	tx, err := ds.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if err := transactionFunc(ctx, newTx(tx)); err != nil {
		return err
	}

	return tx.Commit(ctx)
}
