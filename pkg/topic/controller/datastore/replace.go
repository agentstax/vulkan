package datastore

import (
	"context"
	"fmt"
	"time"

	"github.com/agentstax/vulkan/pkg/topic"
)

// replaceTopicConfig overwrites an already-registered topic's mutable config
// with data's: the newest declaration wins.
// partition_size is not mutable config.
func (d *TopicDatastore) replaceTopicConfig(ctx context.Context, found *TopicData, data *TopicData) (*TopicData, error) {
	if found.PartitionSize != data.PartitionSize {
		return nil, fmt.Errorf("%w: topic %s version %d: partition_size is fixed at %d, got %d",
			topic.ErrTopicConfigMismatch, found.Name, found.SchemaVersion, found.PartitionSize, data.PartitionSize)
	}

	if !configDiffers(found, data) {
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
		data.RetentionTTLNs,
		data.AllowDropPastCommitted,
		data.IdempotencyKeyTTLNs,
		data.DeliveryLogMode,
	)
	updated, err := d.scanTopicData(row)
	if err != nil {
		return nil, err
	}
	if updated == nil {
		// destroyed between the read and the update
		return nil, nil
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

// configDiffers reports whether the declaration would change any mutable config field.
func configDiffers(found *TopicData, data *TopicData) bool {
	return found.RetentionTTLNs != data.RetentionTTLNs ||
		found.AllowDropPastCommitted != data.AllowDropPastCommitted ||
		found.IdempotencyKeyTTLNs != data.IdempotencyKeyTTLNs ||
		found.DeliveryLogMode != data.DeliveryLogMode
}
