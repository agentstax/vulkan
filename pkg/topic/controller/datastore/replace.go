package datastore

import (
	"context"
	"fmt"
	"time"

	"github.com/agentstax/vulkan/pkg/topic"
)

// replaceTopicConfig overwrites an already-registered topic's mutable config
// with declared's: the newest declaration wins.
// partition_size is not mutable config.
func (d *TopicDatastore) replaceTopicConfig(ctx context.Context, found *TopicData, declared *TopicData) (*TopicData, error) {
	if found.PartitionSize != declared.PartitionSize {
		return nil, fmt.Errorf("%w: topic %s version %d: partition_size is fixed at %d, got %d",
			topic.ErrTopicConfigMismatch, found.Name, found.SchemaVersion, found.PartitionSize, declared.PartitionSize)
	}

	if !configDiffers(found, declared) {
		d.Logger.InfoContext(ctx, "topic registered (already existed)", "topic", found.Name, "topic_id", found.Id, "schema_version", found.SchemaVersion)
		return found, nil
	}

	sql := `
		UPDATE topic
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

	row := d.Datastore.Pool.QueryRow(ctx, sql,
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
		return nil, fmt.Errorf("topic %q version %d was deleted while its declaration was in flight -- rerun the declaration if it should still exist",
			found.Name, found.SchemaVersion)
	}

	// the only signal that two services declare this topic differently
	d.Logger.InfoContext(ctx, "topic registered (config replaced)",
		"topic", updated.Name,
		"topic_id", updated.Id,
		"retention_ttl", time.Duration(updated.RetentionTTLNs),
		"allow_drop_past_committed", updated.AllowDropPastCommitted,
		"idempotency_key_ttl", time.Duration(updated.IdempotencyKeyTTLNs),
		"delivery_log_mode", updated.DeliveryLogMode)
	return updated, nil
}

// configDiffers reports whether the declaration would change any mutable
// config field.
func configDiffers(found *TopicData, declared *TopicData) bool {
	return found.RetentionTTLNs != declared.RetentionTTLNs ||
		found.AllowDropPastCommitted != declared.AllowDropPastCommitted ||
		found.IdempotencyKeyTTLNs != declared.IdempotencyKeyTTLNs ||
		found.DeliveryLogMode != declared.DeliveryLogMode
}
