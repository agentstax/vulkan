package datastore

import (
	"context"
	"fmt"
	"time"

	iTopic "github.com/agentstax/vulkan/internal/topic"
	consumerbase "github.com/agentstax/vulkan/pkg/consumer/base"
	"github.com/agentstax/vulkan/pkg/retry"
	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// Commit frees the range's lease, then records failures and deferred messages
// as sparse delivery rows -- initialBackoff sets how long a freshly parked row
// waits before it's first eligible for ClaimExceptions
// (RecordExceptionFailure's own retry policy takes over on later retries).
// deliveryLogMode gates the parallel delivery_log_<topic_id> audit writes.
// The lease is freed FIRST, token-guarded -- so a reclaimed worker's stale
// commit bails before parking any phantom exception rows.
func (d *MessageConsumerDatastore) Commit(ctx context.Context, topicID int64, groupID int64, token pgtype.UUID, outcomes []OutcomeData, initialBackoff time.Duration, deliveryLogMode topic.DeliveryLogMode) error {
	return d.DatastoreRetry.Wrap(ctx, func() error {
		return d.commit(ctx, topicID, groupID, token, outcomes, initialBackoff, deliveryLogMode)
	})
}

func (d *MessageConsumerDatastore) commit(ctx context.Context, topicID int64, groupID int64, token pgtype.UUID, outcomes []OutcomeData, initialBackoff time.Duration, deliveryLogMode topic.DeliveryLogMode) error {
	tx, err := d.Datastore.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	freeSql := `
		DELETE FROM lease
		WHERE consumer_group_id = $1
			AND token = $2;
	`

	tag, err := tx.Exec(ctx, freeSql, groupID, token)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return consumerbase.ErrLeaseLost
	}

	// no ON CONFLICT needed: only the worker whose token still matches the lease
	// reaches this INSERT -- a stale worker's DELETE above matches 0 rows and
	// returns before ever running parkSql.
	batch := &pgx.Batch{}
	terminals := queueOutcomes(batch, parkStatement(topicID), logStatement(topicID), groupID, outcomes, initialBackoff, deliveryLogMode)
	if err := execBatch(ctx, tx, batch); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return err // safe for Retry to auto-classify
	}

	if terminals > 0 {
		d.Logger.WarnContext(ctx, "message(s) dead-lettered (unrecoverable, will not be retried)", "group_id", groupID, "topic_id", topicID, "count", terminals)
	}
	return nil
}

// PartialCommit narrows a still-open lease to lastProcessed and parks whatever
// resolved before an interruption. The lease token isn't freed, it
// naturally expires and gets reclaimed.
func (d *MessageConsumerDatastore) PartialCommit(ctx context.Context, topicID int64, groupID int64, token pgtype.UUID, lastProcessed int64, outcomes []OutcomeData, initialBackoff time.Duration, deliveryLogMode topic.DeliveryLogMode) error {
	return d.DatastoreRetry.Wrap(ctx, func() error {
		return d.partialCommit(ctx, topicID, groupID, token, lastProcessed, outcomes, initialBackoff, deliveryLogMode)
	})
}

func (d *MessageConsumerDatastore) partialCommit(ctx context.Context, topicID int64, groupID int64, token pgtype.UUID, lastProcessed int64, outcomes []OutcomeData, initialBackoff time.Duration, deliveryLogMode topic.DeliveryLogMode) error {
	tx, err := d.Datastore.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// narrow lease range -- the untouched suffix (lastProcessed, high] stays
	// leased under the same token until it expires and is reclaimed. Unlike
	// commit's DELETE, this UPDATE doesn't consume the row -- a retry's own
	// UPDATE still matches it, so it reaches the park statement again. See the
	// recorded-anything guard below.
	truncateSql := `
		UPDATE lease
		SET low = $3
		WHERE consumer_group_id = $1
			AND token = $2;
	`

	tag, err := tx.Exec(ctx, truncateSql, groupID, token, lastProcessed)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return consumerbase.ErrLeaseLost
	}

	// same parking shape as commit -- only the lease-side effect differs.
	batch := &pgx.Batch{}
	terminals := queueOutcomes(batch, parkStatement(topicID), logStatement(topicID), groupID, outcomes, initialBackoff, deliveryLogMode)
	if err := execBatch(ctx, tx, batch); err != nil {
		return err
	}

	// the one genuinely ambiguous point -- a blip AT Commit means we lost the
	// ack, not whether it landed. Unlike commit, truncateSql's UPDATE isn't
	// self-consuming, so a retry that already landed would reach the park
	// statement (or the log row's PK) again -- only safe to retry when nothing
	// was recorded.
	if err := tx.Commit(ctx); err != nil {
		if len(outcomes) > 0 {
			return retry.NewPermanentError(err)
		}
		return err // nothing recorded -- safe for Retry to auto-classify
	}

	if terminals > 0 {
		d.Logger.WarnContext(ctx, "message(s) dead-lettered (unrecoverable, will not be retried)", "group_id", groupID, "topic_id", topicID, "count", terminals)
	}
	return nil
}

func parkStatement(topicID int64) string {
	return fmt.Sprintf(`
		INSERT INTO %s (consumer_group_id, message_id, status, attempts, can_run_after, last_error)
		VALUES (
			$1,
			$2,
			$3,
			0,
			now() + make_interval(secs => $5),
			$4
		);
	`, iTopic.DeliveryTable(topicID))
}

// freshly parked rows are always the first recorded attempt (0)
func logStatement(topicID int64) string {
	return fmt.Sprintf(`
		INSERT INTO %s (consumer_group_id, message_id, attempt, status, error)
		VALUES ($1, $2, 0, $3, $4);
	`, iTopic.DeliveryLogTable(topicID))
}

// queueOutcomes queues one park + one log statement per resolved message, sent
// as a single pipelined round trip. Returns how many parked 'dead'.
// OutcomeSuperseded and OutcomeSuccess park nothing -- they record a log row only.
func queueOutcomes(batch *pgx.Batch, parkSql string, logSql string, groupID int64, outcomes []OutcomeData, initialBackoff time.Duration, deliveryLogMode topic.DeliveryLogMode) int {
	terminals := 0
	for _, outcome := range outcomes {
		switch outcome.Kind {
		case OutcomeException:
			batch.Queue(parkSql, groupID, outcome.MessageId, "ready", outcome.Err, initialBackoff.Seconds())
		case OutcomeTerminal:
			batch.Queue(parkSql, groupID, outcome.MessageId, "dead", outcome.Err, initialBackoff.Seconds())
			terminals++
		case OutcomeDeferred:
			batch.Queue(parkSql, groupID, outcome.MessageId, "deferred", nil, 0.0)
		}
		if deliveryLogMode != topic.DeliveryLogModeOff {
			batch.Queue(logSql, groupID, outcome.MessageId, outcomeLogStatus(outcome.Kind), outcome.Err)
		}
	}
	return terminals
}

// a park's delivery_log status: both failure kinds log as 'failure', the other
// two log under their own name.
func outcomeLogStatus(kind OutcomeKind) string {
	switch kind {
	case OutcomeException, OutcomeTerminal:
		return "failure"
	default:
		return string(kind)
	}
}

// execBatch sends every queued statement as one pipelined round trip instead
// of an Exec per statement.
func execBatch(ctx context.Context, tx pgx.Tx, batch *pgx.Batch) error {
	if batch.Len() == 0 {
		return nil
	}

	br := tx.SendBatch(ctx, batch)
	for range batch.Len() {
		if _, err := br.Exec(); err != nil {
			br.Close()
			return err
		}
	}
	return br.Close()
}
