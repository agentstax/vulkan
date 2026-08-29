package datastore

import (
	"context"
	"fmt"
	"time"

	iTopic "github.com/agentstax/vulkan/internal/topic"
	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/jackc/pgx/v5"
)

// Claim claims 'ready', expired 'inflight', and 'deferred' rows whose
// failures (attempts - delays) are under maxRetries. A leased message key
// excludes its rows.
func (d *ExceptionConsumerGroupDatastore) Claim(ctx context.Context, topicId int64, groupId int64, limit int, maxRetries int, leaseDuration time.Duration, deliveryLogMode topic.DeliveryLogMode) ([]ExceptionData, error) {
	var claimed []ExceptionData
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		claimed, err = d.claim(ctx, topicId, groupId, limit, maxRetries, leaseDuration, deliveryLogMode)
		return err
	})
	return claimed, err
}

func (d *ExceptionConsumerGroupDatastore) claim(ctx context.Context, topicId int64, groupId int64, limit int, maxRetries int, leaseDuration time.Duration, deliveryLogMode topic.DeliveryLogMode) ([]ExceptionData, error) {
	var claimSql string
	if deliveryLogMode == topic.DeliveryLogModeOff {
		claimSql = fmt.Sprintf(`
			-- vulkan: exceptionconsumer.claim
			WITH claimed AS (
				UPDATE %[1]s
				SET
					status = 'inflight',
					lease_token = gen_random_uuid(),
					lease_expires_at = now() + make_interval(secs => $3),
					attempts = attempts + 1,
					updated_at = now()
				WHERE (consumer_group_id, message_id) IN
				(
					SELECT d.consumer_group_id, d.message_id FROM %[1]s d
					WHERE d.consumer_group_id = $1
						AND d.attempts - d.delays < $5
						AND (
							(d.status = 'ready' AND d.can_run_after <= now()) OR
							(d.status = 'inflight' AND d.lease_expires_at < now()) OR
							(d.status = 'deferred')
						)
						-- never claim a row whose message key is under an unexpired lease
						AND NOT EXISTS (
							SELECT 1
							FROM %[3]s kl
							JOIN %[2]s m ON m.id = d.message_id
							WHERE kl.consumer_group_id = d.consumer_group_id
								AND kl.message_key = m.message_key
								AND kl.expires_at >= now()
						)
					ORDER BY d.message_id
					LIMIT $2
					FOR UPDATE OF d SKIP LOCKED
				)
				RETURNING consumer_group_id, message_id, attempts, delays, lease_token, lease_expires_at
			)
			SELECT
				c.consumer_group_id,
				$4::bigint AS topic_id,
				c.message_id,
				c.attempts,
				c.delays,
				c.lease_token,
				c.lease_expires_at,
				m.payload,
				m.created_at,
				COALESCE(m.routing_key, '') AS routing_key,
				COALESCE(m.message_key, '') AS message_key,
				COALESCE(m.compaction_rank, 0) AS compaction_rank,
				(m.compaction_rank IS NOT NULL) AS compacted,
				m.options
			FROM claimed c
			JOIN %[2]s m ON m.id = c.message_id
			ORDER BY c.message_id;
		`, iTopic.ExceptionQueueTable(topicId), iTopic.MessageLogTable(topicId), iTopic.MessageKeyLeaseTable(topicId))
	} else {
		// eligible is split out so it can remember each row's pre-claim status
		// and attempts -- the expired_logged CTE needs both, atomically with
		// the claim itself.
		claimSql = fmt.Sprintf(`
			-- vulkan: exceptionconsumer.claim
			WITH eligible AS (
				SELECT d.consumer_group_id, d.message_id, d.status, d.attempts
				FROM %[1]s d
				WHERE d.consumer_group_id = $1
					AND d.attempts - d.delays < $5
					AND (
						(d.status = 'ready' AND d.can_run_after <= now()) OR
						(d.status = 'inflight' AND d.lease_expires_at < now()) OR
						(d.status = 'deferred')
					)
					-- never claim a row whose message key is under an unexpired lease
					AND NOT EXISTS (
						SELECT 1
						FROM %[4]s kl
						JOIN %[2]s m ON m.id = d.message_id
						WHERE kl.consumer_group_id = d.consumer_group_id
							AND kl.message_key = m.message_key
							AND kl.expires_at >= now()
					)
				ORDER BY d.message_id
				LIMIT $2
				FOR UPDATE OF d SKIP LOCKED
			), claimed AS (
				UPDATE %[1]s d
				SET
					status = 'inflight',
					lease_token = gen_random_uuid(),
					lease_expires_at = now() + make_interval(secs => $3),
					attempts = d.attempts + 1,
					updated_at = now()
				FROM eligible e
				WHERE d.consumer_group_id = e.consumer_group_id
					AND d.message_id = e.message_id
				RETURNING d.consumer_group_id, d.message_id, d.attempts, d.delays, d.lease_token, d.lease_expires_at
			), expired_logged AS (
				-- an expired 'inflight' row is a claim nobody recorded: its
				-- current attempt number is provably absent from the log (any
				-- recorded outcome would have moved the row off 'inflight')
				INSERT INTO %[3]s (consumer_group_id, message_id, attempt, status, error)
				SELECT e.consumer_group_id, e.message_id, e.attempts, 'expired', 'delivery lease expired before an outcome was recorded'
				FROM eligible e
				WHERE e.status = 'inflight'
			)
			SELECT
				c.consumer_group_id,
				$4::bigint AS topic_id,
				c.message_id,
				c.attempts,
				c.delays,
				c.lease_token,
				c.lease_expires_at,
				m.payload,
				m.created_at,
				COALESCE(m.routing_key, '') AS routing_key,
				COALESCE(m.message_key, '') AS message_key,
				COALESCE(m.compaction_rank, 0) AS compaction_rank,
				(m.compaction_rank IS NOT NULL) AS compacted,
				m.options
			FROM claimed c
			JOIN %[2]s m ON m.id = c.message_id
			ORDER BY c.message_id;
		`, iTopic.ExceptionQueueTable(topicId), iTopic.MessageLogTable(topicId), iTopic.DeliveryLogTable(topicId), iTopic.MessageKeyLeaseTable(topicId))
	}

	rows, err := d.Datastore.Pool.Query(ctx, claimSql, groupId, limit, leaseDuration.Seconds(), topicId, maxRetries)
	if err != nil {
		return nil, err
	}

	return pgx.CollectRows(rows, pgx.RowToStructByName[ExceptionData])
}

// RenewLease extends a claim the caller already won.
// false -> the lease was taken over by another claim.
func (d *ExceptionConsumerGroupDatastore) RenewLease(ctx context.Context, exception *ExceptionData, duration time.Duration) (bool, error) {
	var renewed bool
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		renewed, err = d.renewLease(ctx, exception, duration)
		return err
	})
	return renewed, err
}

func (d *ExceptionConsumerGroupDatastore) renewLease(ctx context.Context, exception *ExceptionData, duration time.Duration) (bool, error) {
	sql := fmt.Sprintf(`
		-- vulkan: exceptionconsumer.renewLease
		UPDATE %s
		SET
			lease_expires_at = now() + make_interval(secs => $4),
			updated_at = now()
		WHERE consumer_group_id = $1
			AND message_id = $2
			AND lease_token = $3;
	`, iTopic.ExceptionQueueTable(exception.TopicId))

	tag, err := d.Datastore.Pool.Exec(ctx, sql, exception.ConsumerGroupId, exception.MessageId, exception.LeaseToken, duration.Seconds())
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}
