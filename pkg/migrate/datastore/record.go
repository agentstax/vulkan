package datastore

import (
	"context"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/datastore"
)

func (d *MigrateDatastore) RecordSuccess(ctx context.Context, q datastore.Querier, owner common.Owner, version int64) error {
	_, err := q.Exec(ctx,
		`INSERT INTO migration_log (topic_id, consumer_group_id, migration_version, status) VALUES ($1, $2, $3, 'success');`,
		owner.TopicIdColumn(), owner.ConsumerGroupIdColumn(), version)
	return err
}

// TryRecordFailure commits a diagnostic failure row after a step rolled back --
// best-effort, on a fresh context so the cancel that caused the failure doesn't
// also drop the record.
func (d *MigrateDatastore) TryRecordFailure(ctx context.Context, q datastore.Querier, owner common.Owner, version int64, cause error) {
	ctx = context.WithoutCancel(ctx)
	err := d.Retry.Wrap(ctx, func() error {
		_, e := q.Exec(ctx,
			`INSERT INTO migration_log (topic_id, consumer_group_id, migration_version, status, error) VALUES ($1, $2, $3, 'failure', $4);`,
			owner.TopicIdColumn(), owner.ConsumerGroupIdColumn(), version, cause.Error())
		return e
	})
	if err != nil {
		d.Logger.ErrorContext(ctx, "could not record migration failure", "owner", owner.Kind(), "name", owner.Name, "topic_id", owner.TopicId, "group_id", owner.ConsumerGroupId, "version", version, "cause", cause.Error(), "record_error", err.Error())
	}
}
