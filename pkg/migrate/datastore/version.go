package datastore

import (
	"context"
	"errors"

	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Version is an entity's latest-by-id success row -- latest-by-id, NOT MAX, so
// a downgrade (which records a LOWER version) reads back correctly.
//
// There is no implied baseline but every entity is recorded at creation.
func Version(ctx context.Context, q datastore.Querier, entityType string, entityId int64) (int64, error) {
	var v int64
	err := q.QueryRow(ctx, `
		SELECT schema_version FROM schema_log
		WHERE entity_type = $1 AND entity_id = $2 AND status = 'success'
		ORDER BY id DESC
		LIMIT 1;
	`, entityType, entityId).Scan(&v)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, ErrNotRegistered
		}
		// 42P01 = table does not exist
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "42P01" {
			return 0, ErrNotRegistered
		}
		return 0, err
	}
	return v, nil
}
