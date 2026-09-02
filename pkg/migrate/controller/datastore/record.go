package datastore

import (
	"context"
	"fmt"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/datastore"
)

func (d *MigrateDatastore) recordSuccess(ctx context.Context, q datastore.Querier, owner *common.Owner, version int64, minCompatibleVersion int64) error {
	sql := fmt.Sprintf(`
		-- vulkan: migrate.recordSuccess
		INSERT INTO %[1]s.migration_log (system_id, topic_id, consumer_group_id, migration_version, min_compatible_version, status) VALUES ($1, $2, $3, $4, $5, 'success');
	`, d.Datastore.Schema)
	_, err := q.Exec(ctx, sql,
		owner.SystemIdColumn(), owner.TopicIdColumn(), owner.ConsumerGroupIdColumn(), version, minCompatibleVersion)
	return err
}

// TryRecordFailure commits a diagnostic failure row after a step rolled back --
// best-effort, on a fresh context so the cancel that caused the failure doesn't
// also drop the record.
func (d *MigrateDatastore) TryRecordFailure(ctx context.Context, q datastore.Querier, owner *common.Owner, version int64, cause error) {
	ctx = context.WithoutCancel(ctx)
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		sql := fmt.Sprintf(`
			-- vulkan: migrate.TryRecordFailure
			INSERT INTO %[1]s.migration_log (system_id, topic_id, consumer_group_id, migration_version, status, error) VALUES ($1, $2, $3, $4, 'failure', $5);
		`, d.Datastore.Schema)
		_, e := q.Exec(ctx, sql,
			owner.SystemIdColumn(), owner.TopicIdColumn(), owner.ConsumerGroupIdColumn(), version, cause.Error())
		return e
	})
	if err != nil {
		d.Logger.ErrorContext(ctx, "could not record migration failure", "owner", owner.Name, "owner_kind", owner.Kind(), "topic_id", owner.TopicId, "group_id", owner.ConsumerGroupId, "version", version, "cause", cause, "error", err)
	}
}
