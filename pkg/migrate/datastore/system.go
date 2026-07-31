package datastore

import (
	"context"
	"errors"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// SystemOwner resolves the singleton system row to its owner. Returns
// ErrNotRegistered if the row (or the table itself, 42P01) isn't there.
func SystemOwner(ctx context.Context, q datastore.Querier) (*common.Owner, error) {
	var id int64
	if err := q.QueryRow(ctx, `SELECT id FROM system;`).Scan(&id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotRegistered
		}
		// 42P01 = table does not exist
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "42P01" {
			return nil, ErrNotRegistered
		}
		return nil, err
	}
	return common.NewSystemOwner(id)
}
