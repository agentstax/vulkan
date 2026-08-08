package producer

import (
	"context"

	coredatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Tx is the surface handed to a ProducerFunc/TransactionFunc closure
// allows us to limit users from doing unintended things and also has
// the nice added benefit of not needing to import pgx lib from callers
type Tx interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	CopyFrom(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error)

	// Raw returns the underlying pgx.Tx, for anything outside this
	// interface's surface (SendBatch, LargeObjects, Prepare, a nested
	// Begin). Escape hatch, not the default path.
	Raw() pgx.Tx
}

type vulkanTx struct {
	pgx.Tx
}

func newVulkanTx(tx pgx.Tx) Tx {
	return &vulkanTx{tx}
}

func (t *vulkanTx) Raw() pgx.Tx {
	return t.Tx
}

// InTransaction opens one transaction, runs transactionFunc against it, and
// commits -- the way to publish to multiple targets atomically via ProduceInTx.
//
// It does not retry -- a transient blip or an ambiguous commit failure
// surfaces to you as-is. Wrap your own retry loop around it if you want one;
// only you know what's safe to rerun in your closure. Rerunning the whole
// closure is dedup-safe ONLY under caller-supplied IdempotencyKeys -- unset
// keys mint fresh per call, so a rerun double-publishes.
func InTransaction(ctx context.Context, ds *coredatastore.PostgresDatastore, transactionFunc TransactionFunc) error {
	tx, err := ds.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if err := transactionFunc(ctx, newVulkanTx(tx)); err != nil {
		return err
	}

	return tx.Commit(ctx)
}
