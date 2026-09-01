package datastore

import (
	"context"
	"fmt"
	"time"

	"github.com/agentstax/vulkan/pkg/topic"
)

// SweepExpiredIdempotencyKeys drains idempotency_key rows older than ttl for this topic.
func (d *JanitorDatastore) SweepExpiredIdempotencyKeys(ctx context.Context, topicId int64, ttl time.Duration, batchSize int) error {
	return d.DatastoreRetry.Wrap(ctx, func() error {
		return d.sweepExpiredIdempotencyKeys(ctx, topicId, ttl, batchSize)
	})
}

func (d *JanitorDatastore) sweepExpiredIdempotencyKeys(ctx context.Context, topicId int64, ttl time.Duration, batchSize int) error {
	// defensive only, not a keep-forever switch like RetentionTTL:
	// topic registration defaults an unset IdempotencyKeyTTL to 1h,
	// and there's no supported way to opt idempotency_key rows out of
	// being swept
	if ttl <= 0 {
		return nil
	}

	cutoff := time.Now().Add(-ttl)

	// protect against any potential infinite loops
	const maxIdempotencyKeySweepBatches = 1000
	for range maxIdempotencyKeySweepBatches {
		swept, err := d.sweepIdempotencyKeysBatch(ctx, topicId, cutoff, batchSize)
		if err != nil {
			return err
		}
		if swept < batchSize {
			break // ran out of expired rows
		}
	}

	return nil
}

// sweepIdempotencyKeysBatch deletes up to batchSize expired rows from this
// topic's own idempotency_key_<id> table. created_at (not idempotency_key)
// is the cutoff column -- a caller-supplied key isn't guaranteed to be a
// time-ordered UUIDv7 the way the auto-generated default is, so only the
// server-assigned timestamp is trustworthy for this.
func (d *JanitorDatastore) sweepIdempotencyKeysBatch(ctx context.Context, topicId int64, cutoff time.Time, batchSize int) (int, error) {
	sql := fmt.Sprintf(`
		-- vulkan: topicjanitor.sweepIdempotencyKeysBatch
		DELETE FROM %s
		WHERE idempotency_key IN (
			SELECT idempotency_key FROM %s
			WHERE created_at < $1
			LIMIT $2
		);
	`, topic.IdempotencyKeyTable(topicId), topic.IdempotencyKeyTable(topicId))

	tag, err := d.Datastore.Pool.Exec(ctx, sql, cutoff, batchSize)
	if err != nil {
		return 0, err
	}

	return int(tag.RowsAffected()), nil
}
