package datastore

import (
	"context"
	"fmt"
	"time"

	"github.com/agentstax/vulkan/pkg/topic"
)

// replaceConfig overwrites an already-registered topic's mutable config
// with declared's -- the newest declaration wins -- and appends the new
// snapshot to topic_config_log in the same transaction.
// partition_size is not mutable config.
func (d *TopicDatastore) replaceConfig(ctx context.Context, found *TopicData, declared *TopicData, declaredBy string) (*TopicData, error) {
	if found.PartitionSize != declared.PartitionSize {
		return nil, topic.ErrTopicConfigMismatch.With(
			"topic", found.Name, "version", found.SchemaVersion,
			"existing_partition_size", found.PartitionSize, "declared_partition_size", declared.PartitionSize)
	}

	changes := configChanges(found, declared)
	if len(changes) == 0 {
		d.Logger.InfoContext(ctx, "topic registered (already existed)", "topic", found.Name, "topic_id", found.Id, "schema_version", found.SchemaVersion)
		return found, nil
	}

	tx, err := d.Datastore.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	sql := `
		-- vulkan: topic.replaceConfig
		UPDATE topic_config
		SET
			retention_ttl_ns = $2,
			allow_drop_past_committed = $3,
			idempotency_key_ttl_ns = $4,
			delivery_log_mode = $5,
			updated_at = NOW()
		WHERE id = $1
		RETURNING
			id,
			system_id,
			name,
			schema_version,
			partition_size,
			retention_ttl_ns,
			allow_drop_past_committed,
			idempotency_key_ttl_ns,
			delivery_log_mode,
			created_at,
			updated_at;
	`
	row := tx.QueryRow(ctx, sql,
		found.Id,
		declared.RetentionTTLNs,
		declared.AllowDropPastCommitted,
		declared.IdempotencyKeyTTLNs,
		declared.DeliveryLogMode,
	)
	updated, err := d.scanTopicData(row)
	if err != nil {
		return nil, err
	}
	if updated == nil {
		return nil, topic.ErrTopicDeclarationInterrupted.With("topic", found.Name, "version", found.SchemaVersion)
	}

	if err := d.appendTopicLog(ctx, tx, updated, declaredBy); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	// the only signal that two services declare this topic differently
	d.Logger.InfoContext(ctx, "topic registered (config replaced)",
		append([]any{"topic", updated.Name, "topic_id", updated.Id, "schema_version", updated.SchemaVersion}, changes...)...)
	return updated, nil
}

// ***************
// *** HELPERS ***
// ***************

// configChanges is every mutable config field the declaration would change,
// as log args. Empty means the declaration matches what is stored.
func configChanges(found *TopicData, declared *TopicData) []any {
	var changes []any
	if found.RetentionTTLNs != declared.RetentionTTLNs {
		changes = append(changes, "retention_ttl", replaced(time.Duration(found.RetentionTTLNs), time.Duration(declared.RetentionTTLNs)))
	}
	if found.AllowDropPastCommitted != declared.AllowDropPastCommitted {
		changes = append(changes, "allow_drop_past_committed", replaced(found.AllowDropPastCommitted, declared.AllowDropPastCommitted))
	}
	if found.IdempotencyKeyTTLNs != declared.IdempotencyKeyTTLNs {
		changes = append(changes, "idempotency_key_ttl", replaced(time.Duration(found.IdempotencyKeyTTLNs), time.Duration(declared.IdempotencyKeyTTLNs)))
	}
	if found.DeliveryLogMode != declared.DeliveryLogMode {
		changes = append(changes, "delivery_log_mode", replaced(found.DeliveryLogMode, declared.DeliveryLogMode))
	}
	return changes
}

// replaced renders one field's change as the log line carries it: old -> new.
func replaced(stored any, declared any) string {
	return fmt.Sprintf("%v -> %v", stored, declared)
}
