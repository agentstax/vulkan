package datastore

import (
	"context"
	"fmt"
	"time"

	"github.com/agentstax/vulkan/internal/topic"
	"github.com/jackc/pgx/v5"
)

// ClaimExceptions claims 'ready', expired 'inflight', and 'deferred' rows up
// to maxRetries attempts. A leased compaction key excludes its rows.
func (d *ExceptionConsumerDatastore) ClaimExceptions(ctx context.Context, topicID int64, groupID int64, limit int, maxRetries int, leaseDuration time.Duration, disableDeliveryLog bool) ([]ExceptionData, error) {
	var claimed []ExceptionData
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		claimed, err = d.claimExceptions(ctx, topicID, groupID, limit, maxRetries, leaseDuration, disableDeliveryLog)
		return err
	})
	return claimed, err
}

func (d *ExceptionConsumerDatastore) claimExceptions(ctx context.Context, topicID int64, groupID int64, limit int, maxRetries int, leaseDuration time.Duration, disableDeliveryLog bool) ([]ExceptionData, error) {
	var claimSql string
	if disableDeliveryLog {
		claimSql = fmt.Sprintf(`
			WITH claimed AS (
				UPDATE %[1]s
				SET
					status = 'inflight',
					lease_token = gen_random_uuid(),
					lease_until = now() + make_interval(secs => $3),
					attempts = attempts + 1,
					updated_at = now()
				WHERE (consumer_group_id, message_id) IN
				(
					SELECT d.consumer_group_id, d.message_id FROM %[1]s d
					WHERE d.consumer_group_id = $1
						AND d.attempts < $5
						AND (
							(d.status = 'ready' AND d.can_run_after <= now()) OR
							(d.status = 'inflight' AND d.lease_until < now()) OR
							(d.status = 'deferred')
						)
						-- never claim a row whose compaction key is under an unexpired key_lease
						AND NOT EXISTS (
							SELECT 1
							FROM key_lease kl
							JOIN %[2]s m ON m.id = d.message_id
							WHERE kl.consumer_group_id = d.consumer_group_id
								AND kl.compaction_key = m.compaction_key
								AND kl.expires_at >= now()
						)
					ORDER BY d.message_id
					LIMIT $2
					FOR UPDATE OF d SKIP LOCKED
				)
				RETURNING consumer_group_id, message_id, attempts, lease_token, lease_until
			)
			SELECT
				c.consumer_group_id,
				$4::bigint AS topic_id,
				c.message_id,
				c.attempts,
				c.lease_token,
				c.lease_until,
				m.payload,
				m.created_at,
				COALESCE(m.routing_key, '') AS routing_key,
				COALESCE(m.compaction_key, '') AS compaction_key,
				m.compaction_rank,
				m.options
			FROM claimed c
			JOIN %[2]s m ON m.id = c.message_id
			ORDER BY c.message_id;
		`, topic.DeliveryTable(topicID), topic.MessageLogTable(topicID))
	} else {
		// eligible is split out so it can remember each row's pre-claim status
		// and attempts -- the expired_logged CTE needs both, atomically with
		// the claim itself.
		claimSql = fmt.Sprintf(`
			WITH eligible AS (
				SELECT d.consumer_group_id, d.message_id, d.status, d.attempts
				FROM %[1]s d
				WHERE d.consumer_group_id = $1
					AND d.attempts < $5
					AND (
						(d.status = 'ready' AND d.can_run_after <= now()) OR
						(d.status = 'inflight' AND d.lease_until < now()) OR
						(d.status = 'deferred')
					)
					-- never claim a row whose compaction key is under an unexpired key_lease
					AND NOT EXISTS (
						SELECT 1
						FROM key_lease kl
						JOIN %[2]s m ON m.id = d.message_id
						WHERE kl.consumer_group_id = d.consumer_group_id
							AND kl.compaction_key = m.compaction_key
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
					lease_until = now() + make_interval(secs => $3),
					attempts = d.attempts + 1,
					updated_at = now()
				FROM eligible e
				WHERE d.consumer_group_id = e.consumer_group_id
					AND d.message_id = e.message_id
				RETURNING d.consumer_group_id, d.message_id, d.attempts, d.lease_token, d.lease_until
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
				c.lease_token,
				c.lease_until,
				m.payload,
				m.created_at,
				COALESCE(m.routing_key, '') AS routing_key,
				COALESCE(m.compaction_key, '') AS compaction_key,
				m.compaction_rank,
				m.options
			FROM claimed c
			JOIN %[2]s m ON m.id = c.message_id
			ORDER BY c.message_id;
		`, topic.DeliveryTable(topicID), topic.MessageLogTable(topicID), topic.DeliveryLogTable(topicID))
	}

	rows, err := d.Datastore.Pool.Query(ctx, claimSql, groupID, limit, leaseDuration.Seconds(), topicID, maxRetries)
	if err != nil {
		return nil, err
	}

	return pgx.CollectRows(rows, pgx.RowToStructByName[ExceptionData])
}

// RenewExceptionLease extends a claim the caller already won.
// false -> the lease was taken over by another claim.
func (d *ExceptionConsumerDatastore) RenewExceptionLease(ctx context.Context, exception *ExceptionData, duration time.Duration) (bool, error) {
	var renewed bool
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		renewed, err = d.renewExceptionLease(ctx, exception, duration)
		return err
	})
	return renewed, err
}

func (d *ExceptionConsumerDatastore) renewExceptionLease(ctx context.Context, exception *ExceptionData, duration time.Duration) (bool, error) {
	sql := fmt.Sprintf(`
		UPDATE %s
		SET
			lease_until = now() + make_interval(secs => $4),
			updated_at = now()
		WHERE consumer_group_id = $1
			AND message_id = $2
			AND lease_token = $3;
	`, topic.DeliveryTable(exception.TopicID))

	tag, err := d.Datastore.Pool.Exec(ctx, sql, exception.ConsumerGroupId, exception.MessageId, exception.LeaseToken, duration.Seconds())
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}
