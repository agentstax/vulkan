package datastore

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Querier is what pool, conn, and tx can all do, minus transaction control
// (Begin/Commit/Rollback) -- the surface for statements that run inside a
// boundary the callee doesn't own. *pgxpool.Pool, *pgxpool.Conn, and pgx.Tx
// all satisfy it.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	SendBatch(ctx context.Context, batch *pgx.Batch) pgx.BatchResults
	CopyFrom(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSource pgx.CopyFromSource) (int64, error)
}
