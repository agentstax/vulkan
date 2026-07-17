package producer

import (
	"context"

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
