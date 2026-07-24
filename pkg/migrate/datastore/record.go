package datastore

import (
	"context"

	"github.com/agentstax/vulkan/pkg/datastore"
)

func (d *MigrateDatastore) RecordSuccess(ctx context.Context, q datastore.Querier, entityType string, entityId, version int64) error {
	_, err := q.Exec(ctx,
		`INSERT INTO schema_log (entity_type, entity_id, schema_version, status) VALUES ($1, $2, $3, 'success');`,
		entityType, entityId, version)
	return err
}

// TryRecordFailure commits a diagnostic failure row after a step rolled back --
// best-effort, on a fresh context so the cancel that caused the failure doesn't
// also drop the record.
func (d *MigrateDatastore) TryRecordFailure(ctx context.Context, q datastore.Querier, entityType string, entityId, version int64, cause error) {
	ctx = context.WithoutCancel(ctx)
	err := d.Retry.Wrap(ctx, func() error {
		_, e := q.Exec(ctx,
			`INSERT INTO schema_log (entity_type, entity_id, schema_version, status, error) VALUES ($1, $2, $3, 'failure', $4);`,
			entityType, entityId, version, cause.Error())
		return e
	})
	if err != nil {
		d.Logger.ErrorContext(ctx, "could not record migration failure", "scope", entityType, "entity_id", entityId, "version", version, "cause", cause.Error(), "record_error", err.Error())
	}
}
