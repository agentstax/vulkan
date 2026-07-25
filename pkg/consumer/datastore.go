package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/agentstax/vulkan/internal/topic"
	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/retry"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

var (
	ErrLeaseLost = errors.New("lease lost: row reclaimed by another consumer")
)

type MessageRow struct {
	Id      int64           `db:"id"`
	Payload json.RawMessage `db:"payload"`
	// Status      string          `db:"status"`
	// Attempts    int             `db:"attempts"`
	// CanRunAfter time.Time       `db:"can_run_after"`
	// LeaseUntil  *time.Time      `db:"lease_until"` // nullable
	// LeaseToken  pgtype.UUID     `db:"lease_token"` // nullable
	// LastError   *string         `db:"last_error"`  // nullable
	CreatedAt time.Time `db:"created_at"`
}

type LeaseRow struct {
	Token         pgtype.UUID `db:"token"`
	ConsumerGroup string      `db:"consumer_group"`
	TopicID       int64       `db:"topic_id"`
	Low           int64       `db:"low"`
	High          int64       `db:"high"`
	Until         time.Time   `db:"until"`
	Reclaims      int         `db:"reclaims"`
}

// Low == High means cursor exists but is already at the proven head (nothing to claim)
type CursorRange struct {
	Low  int64 `db:"low"`
	High int64 `db:"high"`
}

// a leased window of work -- the messages to process plus the lease that guards
// them. the worker frees the lease (Commit) once the whole range is done; the
// lazy roller then advances committed past it.
type ClaimedRange struct {
	Lease    LeaseRow
	Messages []MessageRow
}

// one retryable failure from a claimed range -- parked as 'ready' instead of failing the whole range.
type MessageException struct {
	MessageId int64
	Err       string
}

// one unrecoverable failure from a claimed range -- no retry could ever succeed, parks straight to 'dead'.
type MessageTerminal struct {
	MessageId int64
	Err       string
}

// one exception claimed off the exception window for (re)processing -- the lease
// token guards its resolution the same way LeaseRow's does for a range.
type ClaimedException struct {
	ConsumerGroup string          `db:"consumer_group"`
	TopicID       int64           `db:"topic_id"`
	MessageId     int64           `db:"message_id"`
	Attempts      int             `db:"attempts"`
	LeaseToken    pgtype.UUID     `db:"lease_token"`
	Payload       json.RawMessage `db:"payload"`
}

// DeliveryRow is one (consumer_group, message_id) row of the per-topic
// delivery_<topic_id> table: the mutable per-consumer lifecycle state that
// lives off the immutable message_log. Payload is not stored on the row --
// it's joined back in from message_log at claim time. The lifecycle claim
// path never sets the lease columns (lease_until / lease_token) -- no crash
// recovery there; the exception window is what leases through them.
type DeliveryRow struct {
	ConsumerGroup string          `db:"consumer_group"`
	TopicID       int64           `db:"topic_id"`
	MessageId     int64           `db:"message_id"`
	Payload       json.RawMessage `db:"payload"`
	Status        string          `db:"status"`
	Attempts      int             `db:"attempts"`
}

type ConsumerDatastore[Message any] struct {
	Datastore      *datastore.PostgresDatastore
	DatastoreRetry *retry.DatastoreRetry // default Wrap classification covers everything except Commit/PartialCommit -- classified inline at that call site
	MessageRetry   *retry.Retry          // exception/terminal can_run_after curve -- never Wrap()'d, just CalculateDelay
	Logger         logger.Logger
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewConsumerDatastore[Message any](ds *datastore.PostgresDatastore, cfg *ConsumerDatastoreConfig) (*ConsumerDatastore[Message], error) {
	if ds == nil {
		return nil, errors.New("datastore must not be nil")
	}
	if cfg == nil {
		cfg = &ConsumerDatastoreConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	dsRetry, err := retry.NewDatastoreRetry(cfg.Retry, cfg.Logger)
	if err != nil {
		return nil, err
	}
	messageRetry, err := retry.NewRetry(cfg.MessageRetry, cfg.Logger)
	if err != nil {
		return nil, err
	}

	return &ConsumerDatastore[Message]{
		Datastore:      ds,
		DatastoreRetry: dsRetry,
		MessageRetry:   messageRetry,
		Logger:         cfg.Logger,
	}, nil
}

func (d *ConsumerDatastore[Message]) UpsertCursor(ctx context.Context, topicID int64, consumerGroup string) error {
	tx, err := d.Datastore.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	cursorSql := `
		INSERT INTO cursor (consumer_group, topic_id)
		VALUES ($1, $2)
		ON CONFLICT DO NOTHING;
	`
	if _, err := tx.Exec(ctx, cursorSql, consumerGroup, topicID); err != nil {
		return err
	}

	// a cursor must never exist without a waterline duty
	dutySql := `
		INSERT INTO maintenance (duty, topic_id, consumer_group)
		VALUES ('waterline', $2, $1)
		ON CONFLICT DO NOTHING;
	`
	if _, err := tx.Exec(ctx, dutySql, consumerGroup, topicID); err != nil {
		return err
	}

	return tx.Commit(ctx)
}


// Commit frees the range's lease, then parks any failures as sparse delivery
// rows -- initialBackoff sets how long a freshly parked row waits before it's
// first eligible for ClaimExceptions (RecordExceptionFailure's own
// MessageRetry takes over on later retries). disableDeliveryLog skips the
// parallel delivery_log_<topic_id> audit write.
// The lease is freed FIRST, token-guarded -- so a reclaimed worker's stale
// commit bails before parking any phantom exception rows.
func (d *ConsumerDatastore[Message]) Commit(ctx context.Context, topicID int64, consumerGroup string, token pgtype.UUID, exceptions []MessageException, terminals []MessageTerminal, initialBackoff time.Duration, disableDeliveryLog bool) error {
	return d.DatastoreRetry.Wrap(ctx, func() error {
		return d.commit(ctx, topicID, consumerGroup, token, exceptions, terminals, initialBackoff, disableDeliveryLog)
	})
}

func (d *ConsumerDatastore[Message]) commit(ctx context.Context, topicID int64, consumerGroup string, token pgtype.UUID, exceptions []MessageException, terminals []MessageTerminal, initialBackoff time.Duration, disableDeliveryLog bool) error {
	tx, err := d.Datastore.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// topic_id isn't load-bearing here -- token alone already disambiguates a
	// lease row -- but every lease query stays topic-scoped by convention.
	freeSql := `
		DELETE FROM lease
		WHERE consumer_group = $1
			AND token = $2
			AND topic_id = $3;
	`

	tag, err := tx.Exec(ctx, freeSql, consumerGroup, token, topicID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrLeaseLost
	}

	// no ON CONFLICT needed: only the worker whose token still matches the lease
	// reaches this INSERT -- a stale worker's DELETE above matches 0 rows and
	// returns before ever running parkSql.
	parkSql := fmt.Sprintf(`
		INSERT INTO %s (consumer_group, message_id, status, attempts, can_run_after, last_error)
		VALUES (
			$1,
			$2,
			$3,
			0,
			now() + make_interval(secs => $5),
			$4
		);
	`, topic.DeliveryTable(topicID))

	// freshly parked rows are always the first recorded attempt (0) for this
	logSql := fmt.Sprintf(`
		INSERT INTO %s (consumer_group, message_id, attempt, error)
		VALUES ($1, $2, 0, $3);
	`, topic.DeliveryLogTable(topicID))

	// queued and sent as one pipelined round trip
	batch := &pgx.Batch{}
	for _, e := range exceptions {
		batch.Queue(parkSql, consumerGroup, e.MessageId, "ready", e.Err, initialBackoff.Seconds())
		if !disableDeliveryLog {
			batch.Queue(logSql, consumerGroup, e.MessageId, e.Err)
		}
	}
	for _, t := range terminals {
		batch.Queue(parkSql, consumerGroup, t.MessageId, "dead", t.Err, initialBackoff.Seconds())
		if !disableDeliveryLog {
			batch.Queue(logSql, consumerGroup, t.MessageId, t.Err)
		}
	}
	if err := execBatch(ctx, tx, batch); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return err // safe for Retry to auto-classify
	}

	if len(terminals) > 0 {
		d.Logger.WarnContext(ctx, "message(s) dead-lettered (unrecoverable, will not be retried)", "group", consumerGroup, "topic_id", topicID, "count", len(terminals))
	}
	return nil
}

// PartialCommit narrows a still-open lease to lastProcessed and parks whatever
// resolved before an interruption. The lease token isnt freed, it
// naturally expires and gets reclaimed.
func (d *ConsumerDatastore[Message]) PartialCommit(ctx context.Context, topicID int64, consumerGroup string, token pgtype.UUID, lastProcessed int64, exceptions []MessageException, terminals []MessageTerminal, initialBackoff time.Duration, disableDeliveryLog bool) error {
	return d.DatastoreRetry.Wrap(ctx, func() error {
		return d.partialCommit(ctx, topicID, consumerGroup, token, lastProcessed, exceptions, terminals, initialBackoff, disableDeliveryLog)
	})
}

func (d *ConsumerDatastore[Message]) partialCommit(ctx context.Context, topicID int64, consumerGroup string, token pgtype.UUID, lastProcessed int64, exceptions []MessageException, terminals []MessageTerminal, initialBackoff time.Duration, disableDeliveryLog bool) error {
	tx, err := d.Datastore.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// narrow lease range -- the untouched suffix (lastProcessed, high] stays
	// leased under the same token until it expires and is reclaimed. Unlike
	// commit's DELETE, this UPDATE doesn't consume the row -- a retry's own
	// UPDATE still matches it, so it reaches parkSql again. See the
	// hasParkedRows guard below.
	truncateSql := `
		UPDATE lease
		SET low = $4
		WHERE consumer_group = $1
			AND token = $2
			AND topic_id = $3;
	`

	tag, err := tx.Exec(ctx, truncateSql, consumerGroup, token, topicID, lastProcessed)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrLeaseLost
	}

	// same parking shape as commit -- only the lease-side effect differs.
	parkSql := fmt.Sprintf(`
		INSERT INTO %s (consumer_group, message_id, status, attempts, can_run_after, last_error)
		VALUES (
			$1,
			$2,
			$3,
			0,
			now() + make_interval(secs => $5),
			$4
		);
	`, topic.DeliveryTable(topicID))

	// same first-recorded-attempt convention as commit's own logSql.
	logSql := fmt.Sprintf(`
		INSERT INTO %s (consumer_group, message_id, attempt, error)
		VALUES ($1, $2, 0, $3);
	`, topic.DeliveryLogTable(topicID))

	// queued and sent as one pipelined round trip
	batch := &pgx.Batch{}
	for _, e := range exceptions {
		batch.Queue(parkSql, consumerGroup, e.MessageId, "ready", e.Err, initialBackoff.Seconds())
		if !disableDeliveryLog {
			batch.Queue(logSql, consumerGroup, e.MessageId, e.Err)
		}
	}
	for _, t := range terminals {
		batch.Queue(parkSql, consumerGroup, t.MessageId, "dead", t.Err, initialBackoff.Seconds())
		if !disableDeliveryLog {
			batch.Queue(logSql, consumerGroup, t.MessageId, t.Err)
		}
	}
	if err := execBatch(ctx, tx, batch); err != nil {
		return err
	}

	// the one genuinely ambiguous point -- a blip AT Commit means we lost the
	// ack, not whether it landed. Unlike commit, truncateSql's UPDATE isn't
	// self-consuming, so a retry that already landed would reach parkSql again
	// and hit delivery's PK -- only safe to retry when nothing was parked.
	if err := tx.Commit(ctx); err != nil {
		if len(exceptions)+len(terminals) > 0 {
			return retry.NewPermanentError(err)
		}
		return err // nothing parked -- safe for Retry to auto-classify
	}

	if len(terminals) > 0 {
		d.Logger.WarnContext(ctx, "message(s) dead-lettered (unrecoverable, will not be retried)", "group", consumerGroup, "topic_id", topicID, "count", len(terminals))
	}
	return nil
}

// ForceReclaimRange surrenders a range nobody ever started -- unlike
// PartialCommit this expires the WHOLE lease immediately so the next
// ReclaimWithCursor can pick it straight back up.
func (d *ConsumerDatastore[Message]) ForceReclaimRange(ctx context.Context, topicID int64, consumerGroup string, token pgtype.UUID) error {
	return d.DatastoreRetry.Wrap(ctx, func() error {
		return d.forceReclaimRange(ctx, topicID, consumerGroup, token)
	})
}

func (d *ConsumerDatastore[Message]) forceReclaimRange(ctx context.Context, topicID int64, consumerGroup string, token pgtype.UUID) error {
	// reclaims goes negative on purpose: the next ReclaimWithCursor's
	// unconditional +1 nets it back to 0 -- this must not count as a real reclaim.
	sql := `
		UPDATE lease
		SET
			until = now(),
			reclaims = GREATEST(reclaims - 1, -1), -- should never go under -1
			token = gen_random_uuid()              -- rotate token so any retry matches 0 rows instead of double decrementing
		WHERE consumer_group = $1
			AND token = $2
			AND topic_id = $3;
	`

	tag, err := d.Datastore.Pool.Exec(ctx, sql, consumerGroup, token, topicID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrLeaseLost
	}
	return nil
}

// try to pick up a crashed range (an expired lease) and only claims fresh work
// from the frontier if there's nothing to reclaim -- so crashed ranges drain first.
func (d *ConsumerDatastore[Message]) ClaimMessagesWithCursor(ctx context.Context, topicID int64, consumerGroup string, limit, maxRangeReclaims int, leaseDuration time.Duration, disableDeliveryLog bool) (*ClaimedRange, error) {
	var claimed *ClaimedRange
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		claimed, err = d.claimMessagesWithCursor(ctx, topicID, consumerGroup, limit, maxRangeReclaims, leaseDuration, disableDeliveryLog)
		return err
	})
	return claimed, err
}

func (d *ConsumerDatastore[Message]) claimMessagesWithCursor(ctx context.Context, topicID int64, consumerGroup string, limit, maxRangeReclaims int, leaseDuration time.Duration, disableDeliveryLog bool) (*ClaimedRange, error) {
	reclaimed, err := d.ReclaimWithCursor(ctx, topicID, consumerGroup, limit, maxRangeReclaims, leaseDuration, disableDeliveryLog)
	if err != nil {
		return nil, err
	}
	if reclaimed != nil {
		return reclaimed, nil
	}

	// nothing to reclaim, or the one reclaimable range was poisoned and just got
	// quarantined instead -> try standard fresh claim (nil when caught up)
	return d.FreshClaimMessagesWithCursor(ctx, topicID, consumerGroup, limit, leaseDuration)
}

// grab ONE expired lease and re-reads its exact range so a worker that crashed
// mid-range doesn't strand those offsets. past maxRangeReclaims the range is
// POISON -- quarantine it into the sparse exception window instead of handing it
// out again, so one bad message can't crash-loop the whole range forever.
func (d *ConsumerDatastore[Message]) ReclaimWithCursor(ctx context.Context, topicID int64, consumerGroup string, limit, maxRangeReclaims int, leaseDuration time.Duration, disableDeliveryLog bool) (*ClaimedRange, error) {
	tx, err := d.Datastore.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	// a single in-place UPDATE, not delete+insert -- reclaims accumulates on the
	// SAME row instead of resetting to 0 every time. token still rotates, so a
	// dead worker's stale commit still no-ops the same as before.
	reclaimSql := `
		UPDATE lease
		SET
			reclaims = reclaims + 1,
			until = now() + make_interval(secs => $2),
			token = gen_random_uuid()
		WHERE (token, consumer_group) IN (
			SELECT token, consumer_group FROM lease
			WHERE consumer_group = $1
				AND topic_id = $3
				AND until < now()
			LIMIT 1
			FOR UPDATE SKIP LOCKED
		)
		RETURNING *;
	`

	leaseRows, err := tx.Query(ctx, reclaimSql, consumerGroup, leaseDuration.Seconds(), topicID)
	if err != nil {
		return nil, err
	}

	lease, err := pgx.CollectOneRow(leaseRows, pgx.RowToStructByName[LeaseRow])
	if err != nil {
		// no reclaimable leases where found -> follow normal claim from message_log
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}

		return nil, err
	}

	d.Logger.InfoContext(ctx, "lease reclaimed from expired worker", "group", consumerGroup, "topic_id", topicID, "low", lease.Low, "high", lease.High, "reclaims", lease.Reclaims)

	if lease.Reclaims >= maxRangeReclaims {
		if err := d.quarantine(ctx, tx, consumerGroup, lease, disableDeliveryLog); err != nil {
			return nil, err
		}
		return nil, tx.Commit(ctx)
	}

	msgs, err := d.readMessages(ctx, tx, topicID, consumerGroup, lease.Low, lease.High)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	return &ClaimedRange{Lease: lease, Messages: msgs}, nil
}

// quarantine gives up on retrying a poisoned range as one unit: every message in
// it parks as an independent 'ready' exception (attempts starts fresh at 0 -- a
// separate retry budget from the range's now-exhausted reclaim count), each
// logging its own delivery_log_<topic_id> row same as any other park, and the
// lease frees for good. From here each message lives or dies on its own via the
// exact same exception-window machinery as an ordinary message-loop failure --
// AdvanceWaterline's exception-blocker term pins committed on whichever
// resolves last, so one bad message no longer holds up its siblings forever.
func (d *ConsumerDatastore[Message]) quarantine(ctx context.Context, tx pgx.Tx, consumerGroup string, lease LeaseRow, disableDeliveryLog bool) error {
	d.Logger.WarnContext(ctx, "range quarantined after max reclaims, messages parked as exceptions", "group", consumerGroup, "topic_id", lease.TopicID, "low", lease.Low, "high", lease.High, "reclaims", lease.Reclaims)

	var parkSql string
	if disableDeliveryLog {
		parkSql = fmt.Sprintf(`
			INSERT INTO %s (consumer_group, message_id, status, attempts, last_error)
			SELECT $1, id, 'ready', 0, 'quarantined: range reclaimed too many times'
			FROM %s
			WHERE id > $2
				AND id <= $3;
		`, topic.DeliveryTable(lease.TopicID), topic.MessageLogTable(lease.TopicID))
	} else {
		// parked CTE + INSERT keeps the range-wide park and its delivery_log_<topic_id>
		// rows atomic -- one log row per message parked, same first-recorded-attempt
		// convention (attempt=0) as commit's own logSql.
		parkSql = fmt.Sprintf(`
			WITH parked AS (
				INSERT INTO %[1]s (consumer_group, message_id, status, attempts, last_error)
				SELECT $1, id, 'ready', 0, 'quarantined: range reclaimed too many times'
				FROM %[2]s
				WHERE id > $2
					AND id <= $3
				RETURNING message_id, last_error
			)
			INSERT INTO %[3]s (consumer_group, message_id, attempt, error)
			SELECT $1, message_id, 0, last_error FROM parked;
		`, topic.DeliveryTable(lease.TopicID), topic.MessageLogTable(lease.TopicID), topic.DeliveryLogTable(lease.TopicID))
	}
	if _, err := tx.Exec(ctx, parkSql, consumerGroup, lease.Low, lease.High); err != nil {
		return err
	}

	freeSql := `
		DELETE FROM lease
		WHERE consumer_group = $1
			AND token = $2
			AND topic_id = $3;
	`
	_, err := tx.Exec(ctx, freeSql, consumerGroup, lease.Token, lease.TopicID)
	return err
}

// readMessages reads topicID's message_log rows in (low, high], ordered by id.
func (d *ConsumerDatastore[Message]) readMessages(ctx context.Context, tx pgx.Tx, topicID int64, consumerGroup string, low, high int64) ([]MessageRow, error) {
	sql := fmt.Sprintf(`
		SELECT m.id, m.payload, m.created_at FROM %s m
		WHERE m.id > $1
			AND m.id <= $2
			AND (
				-- no bindings for consumer_group exists
				NOT EXISTS (
					SELECT 1 FROM binding b
					WHERE b.consumer_group = $3
				)
				-- bindings for consumer_group exists and match routing_key pattern
				OR EXISTS (
					SELECT 1 FROM binding b
					WHERE b.consumer_group = $3
						AND m.routing_key ~ b.pattern
				)
				-- if bindings exist but our routing_key does not match any of them
				-- we do not return anything
			)
			AND (
				-- unkeyed rows are never compacted
				m.compaction_key IS NULL
				-- keyed rows are eligible only if they're latest_key's current
				-- pointer for their key -- O(1) lookup, no per-row scan
				OR m.id = (
					SELECT latest_id FROM latest_key
					WHERE topic_id = $4
						AND compaction_key = m.compaction_key
				)
			)
		-- rows MUST come back in id order or a batch LIMIT could
		-- return an arbitrary subset and the cursor would advance past unread offsets
		ORDER BY m.id;
	`, topic.MessageLogTable(topicID))

	rows, err := tx.Query(ctx, sql, low, high, consumerGroup, topicID)
	if err != nil {
		return nil, err
	}

	return pgx.CollectRows(rows, pgx.RowToStructByName[MessageRow])
}

func (d *ConsumerDatastore[Message]) FreshClaimMessagesWithCursor(ctx context.Context, topicID int64, consumerGroup string, limit int, leaseDuration time.Duration) (*ClaimedRange, error) {
	tx, err := d.Datastore.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	// take the (head, xmax) pair the cursorSql gate CTE below proves against.
	//
	// this snapshot must run BEFORE the entire tx's first write
	// If we do any kind of INSERT/UPDATE it will put a txid into pg_current_snapshot
	// that will make it such that the xmax of this snapshot could never be reached
	// by cursorSql xmin because the txid cause by that write can never finish until
	// this entire transaction completes. Basically fresh-pair would never be selected
	// and claims would always have to wait at least a poll tick, slowing things down.
	snapshotSql := fmt.Sprintf(`
		SELECT
			(SELECT COALESCE(MAX(id), 0) FROM %s) AS head,
			pg_snapshot_xmax(pg_current_snapshot())::text AS xmax,
			c.claimed,
			c.settled_head,
			c.pending_head
		FROM cursor c
		WHERE c.consumer_group = $1 AND c.topic_id = $2;
	`, topic.MessageLogTable(topicID))

	var snapshotHead, claimed, settledHead, pendingHead int64
	var snapshotXmax string
	if err := tx.QueryRow(ctx, snapshotSql, consumerGroup, topicID).Scan(&snapshotHead, &snapshotXmax, &claimed, &settledHead, &pendingHead); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("no cursor for group %s on topic %d -- was Register called?", consumerGroup, topicID)
		}
		return nil, err
	}

	// nothing new and nothing provable: this snapshot saw the head we've
	// already proven and fully claimed.
	if snapshotHead == pendingHead && pendingHead == settledHead && claimed == settledHead {
		return nil, nil
	}

	// TODO - projector could likely tracked head in a RWMutex such that it doesn't need to be calculated here
	cursorSql := `
		WITH old_values AS ( -- PG18+ has old / new syntax in returning but we want older version compatibility so use CTE
			SELECT * FROM cursor
			WHERE consumer_group = $1 AND topic_id = $3
			-- must FOR UPDATE, get race if using a basic snapshot read
			-- two same-group workers racing on one cursor row (claimed=0, head=200, limit=100):
			--
			--   worker A: claims (0, 100], txn still open
			--   worker B: takes its snapshot (claimed=0), blocks on A's row lock
			--   worker A: commits
			--   worker B: unblocks; its UPDATE re-checks the row's LATEST version
			--             (claimed=100), so high is correct: 100+100 = 200
			--
			-- but B's low comes from THIS select, and forks on its read mode:
			--
			--   snapshot read:  low = 0   (stale)  -> B returns (0, 200]   -> overlaps A
			--   FOR UPDATE:     low = 100 (latest) -> B returns (100, 200] -> disjoint
			FOR UPDATE
		),
		gate AS (
			-- gate = how far claimed may advance this poll.
			--
			-- the raw MAX(id) is unsafe: BIGSERIAL issues ids at INSERT time,
			-- txns commit in any order, and nothing re-reads below claimed:
			--
			-- EX:
			--
			--   producer A: INSERT id=8, txn stays open
			--   producer B: INSERT id=9, commits
			--   claim to MAX(id)=9 -> reads (0,9], 8 is invisible, skipped
			--   producer A commits -> 8 < claimed forever -> LOST
			--
			-- the fix: claimed only advances to a head PROVEN to have nothing
			-- invisible at or below it. the proof works on a (head, xmax)
			-- pair -- MAX(id) (head) and the next-unissued txid (max), read together
			-- in one EARLIER snapshot (snapshotSql above, or a prior poll that
			-- stored its pair in pending_head/pending_xmax).
			--
			-- EX: proving the pair (head=9, xmax=103) from snapshotSql:
			--
			--   1. the pair says:  every txn that can own an id <= 9 has txid < 103
			--                      (all ids <= 9 were INSERTed before txid 103 was issued)
			--   2. since then:     txns have kept finishing, so xmin -- the oldest
			--                      txid still running -- rises toward 103 as they do
			--   3. this query:     if it sees xmin >= 103, every txid < 103 is finished
			--   4. therefore:      every txn that can own an id <= 9 is finished --
			--                      anything committing at or below 9 already has, so
			--                      claiming through 9 skips nothing
			--
			-- gate takes the best proven head available:
			--   settled_head     -- wins when neither pair proves (a txn seen by
			--                       both snapshots is still open, e.g. a long
			--                       ProduceInTx) -- claims hold at the last proven
			--                       head until it closes
			--   the fresh pair   -- $4/$5, wins when everything running at
			--                       snapshotSql finished before this query ran --
			--                       the quiet path, claims land in the same poll
			--                       as the produce
			--   the stored pair  -- wins under nonstop traffic: the fresh pair is
			--                       only microseconds old, too young for its fenced
			--                       txns to have finished, but the stored pair has
			--                       had a full poll interval for that -- xmin has
			--                       passed its xmax. claiming through it claims up
			--                       to where the log stood a poll ago, so fresh
			--                       messages wait one more poll if this is used
			SELECT GREATEST(
				o.settled_head,
				CASE WHEN pg_snapshot_xmin(pg_current_snapshot()) >= $5::xid8 -- $5 is snapshotXmax
					THEN $4 ELSE 0 END,                                         -- $4 is snapshotHead
				CASE WHEN o.pending_xmax IS NOT NULL
						AND pg_snapshot_xmin(pg_current_snapshot()) >= o.pending_xmax
					THEN o.pending_head ELSE 0 END
			) AS head
			FROM old_values o
		),
		updated AS (
			UPDATE cursor
			SET
				-- advance by up to batchLimit, capped at the proven head.
				claimed = LEAST(cursor.claimed + $2, gate.head),
				-- cache this poll's proof: a later poll where neither pair
				-- proves claims up to this instead.
				settled_head = gate.head,
				-- store the fresh pair for the next poll: ideally its txns will
				-- have finished by then, making it the next provable head.
				-- GREATEST so a racing peer's older pair can't overwrite a newer one
				pending_head = GREATEST(cursor.pending_head, $4),
				pending_xmax = GREATEST(cursor.pending_xmax, $5::xid8) -- also skips the initial NULL
			FROM old_values, gate
			WHERE cursor.consumer_group = $1
				AND cursor.topic_id = $3
			RETURNING
				old_values.claimed AS low,
				cursor.claimed AS high
		)
		-- updated always fires when the cursor row exists (the pending columns
		-- store unconditionally), so:
		--
		--   state                        | rows   low    high   meaning
		--   claimed=100, proven=200      | 1      100    200    claim (100, 200]
		--   claimed=200, proven=200      | 1      200    200    caught up (low = high)
		--   no cursor row                | 0      -      -      row deleted since the
		--                                                       snapshot read it -> error
		--
		SELECT u.low, u.high FROM updated u;
	`

	cursorRows, err := tx.Query(ctx, cursorSql, consumerGroup, limit, topicID, snapshotHead, snapshotXmax)
	if err != nil {
		return nil, err
	}

	claimedRange, err := pgx.CollectOneRow(cursorRows, pgx.RowToStructByName[CursorRange])
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// if we didnt error a consumer with no cursor row would otherwise
			// poll forever looking caught up while messages accumulate
			return nil, fmt.Errorf("no cursor for group %s on topic %d -- was Register called?", consumerGroup, topicID)
		}

		return nil, err
	}

	// at the proven head of message_log ie no messages to process
	if claimedRange.Low == claimedRange.High {
		return nil, nil
	}

	return d.ClaimMessages(
		ctx, tx, topicID, consumerGroup, claimedRange.Low, claimedRange.High, limit, leaseDuration,
	)
}

func (d *ConsumerDatastore[Message]) ClaimMessages(
	ctx context.Context,
	tx pgx.Tx,
	topicID int64,
	consumerGroup string,
	low int64,
	high int64,
	limit int,
	leaseDuration time.Duration,
) (*ClaimedRange, error) {
	// sanity check guard
	if low >= high {
		return nil, errors.New("invalid claimed range")
	}

	// get new lease associated with range
	leaseSql := `
		INSERT INTO lease (consumer_group, topic_id, low, high, until)
		VALUES (
			$1,
			$2,
			$3,
			$4,
			now() + make_interval(secs => $5)
		)
		RETURNING *;
	`

	leaseRows, err := tx.Query(ctx, leaseSql, consumerGroup, topicID, low, high, leaseDuration.Seconds())
	if err != nil {
		return nil, err
	}

	lease, err := pgx.CollectOneRow(leaseRows, pgx.RowToStructByName[LeaseRow])
	if err != nil {
		return nil, err
	}

	msgs, err := d.readMessages(ctx, tx, topicID, consumerGroup, low, high)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	return &ClaimedRange{Lease: lease, Messages: msgs}, nil
}

// ClaimExceptions drains the sparse exception window: kill exhausted delivery
// rows, then claim. The kill backstop is itself a failure record --
// disableDeliveryLog skips its delivery_log_<topic_id> write too.
func (d *ConsumerDatastore[Message]) ClaimExceptions(ctx context.Context, topicID int64, consumerGroup string, limit, maxAttempts int, leaseDuration time.Duration, disableDeliveryLog bool) ([]ClaimedException, error) {
	var claimed []ClaimedException
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		claimed, err = d.claimExceptions(ctx, topicID, consumerGroup, limit, maxAttempts, leaseDuration, disableDeliveryLog)
		return err
	})
	return claimed, err
}

func (d *ConsumerDatastore[Message]) claimExceptions(ctx context.Context, topicID int64, consumerGroup string, limit, maxAttempts int, leaseDuration time.Duration, disableDeliveryLog bool) ([]ClaimedException, error) {
	// an exception that causes a crash loop never resolves normally -- without this
	// backstop it would reclaim forever, pinning committed below it forever.
	var killSql string
	if disableDeliveryLog {
		killSql = fmt.Sprintf(`
			UPDATE %s
			SET
				status = 'dead',
				lease_token = NULL,
				lease_until = NULL,
				updated_at = now(),
				last_error = concat(last_error, ' [killed: crash-loop hit max attempts]')
			WHERE consumer_group = $1
				AND status = 'inflight'
				AND lease_until < now()
				AND attempts >= $2;
		`, topic.DeliveryTable(topicID))
	} else {
		// killed CTE + INSERT keeps the kill and its delivery_log_<topic_id> row
		// atomic in one statement
		killSql = fmt.Sprintf(`
			WITH killed AS (
				UPDATE %[1]s
				SET
					status = 'dead',
					lease_token = NULL,
					lease_until = NULL,
					updated_at = now(),
					last_error = concat(last_error, ' [killed: crash-loop hit max attempts]')
				WHERE consumer_group = $1
					AND status = 'inflight'
					AND lease_until < now()
					AND attempts >= $2
				RETURNING consumer_group, message_id, attempts, last_error
			)
			INSERT INTO %[2]s (consumer_group, message_id, attempt, error)
			SELECT consumer_group, message_id, attempts, last_error FROM killed;
		`, topic.DeliveryTable(topicID), topic.DeliveryLogTable(topicID))
	}
	killTag, err := d.Datastore.Pool.Exec(ctx, killSql, consumerGroup, maxAttempts)
	if err != nil {
		return nil, err
	}
	if killTag.RowsAffected() > 0 {
		d.Logger.WarnContext(ctx, "crash-loop kill backstop fired, exception(s) marked dead", "group", consumerGroup, "topic_id", topicID, "count", killTag.RowsAffected())
	}

	// joins to this topic's own message_log, since delivery stores no payload of its own.
	claimSql := fmt.Sprintf(`
		WITH claimed AS (
			UPDATE %[1]s
			SET
				status = 'inflight',
				lease_token = gen_random_uuid(),
				lease_until = now() + make_interval(secs => $3),
				attempts = attempts + 1,
				updated_at = now()
			WHERE (consumer_group, message_id) IN
			(
				-- claim either:
				--   retryable 'ready' exceptions
				--   expired 'inflight' exceptions
				SELECT consumer_group, message_id FROM %[1]s
				WHERE consumer_group = $1
					AND can_run_after <= now()
					AND (
						status = 'ready' OR
						(status = 'inflight' AND lease_until < now())
					)
				ORDER BY message_id
				LIMIT $2
				FOR UPDATE SKIP LOCKED
			)
			RETURNING consumer_group, message_id, attempts, lease_token
		)
		SELECT
			c.consumer_group,
			$4::bigint AS topic_id,
			c.message_id,
			c.attempts,
			c.lease_token,
			m.payload
		FROM claimed c
		JOIN %[2]s m ON m.id = c.message_id
		ORDER BY c.message_id;
	`, topic.DeliveryTable(topicID), topic.MessageLogTable(topicID))

	rows, err := d.Datastore.Pool.Query(ctx, claimSql, consumerGroup, limit, leaseDuration.Seconds(), topicID)
	if err != nil {
		return nil, err
	}

	return pgx.CollectRows(rows, pgx.RowToStructByName[ClaimedException])
}

// success pop-deletes the row -- same sparse convention as never parking it: no row means resolved.
func (d *ConsumerDatastore[Message]) RecordExceptionSuccess(ctx context.Context, exception *ClaimedException) error {
	return d.DatastoreRetry.Wrap(ctx, func() error {
		return d.recordExceptionSuccess(ctx, exception)
	})
}

func (d *ConsumerDatastore[Message]) recordExceptionSuccess(ctx context.Context, exception *ClaimedException) error {
	sql := fmt.Sprintf(`
		DELETE FROM %s
		WHERE consumer_group = $1
			AND message_id = $2
			AND lease_token = $3;
	`, topic.DeliveryTable(exception.TopicID))

	tag, err := d.Datastore.Pool.Exec(ctx, sql, exception.ConsumerGroup, exception.MessageId, exception.LeaseToken)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrLeaseLost
	}

	return nil
}

// RecordExceptionFailure handles a retried exception's failure: attempts was already
// incremented at claim time, so >= maxAttempts means this was the last try.
func (d *ConsumerDatastore[Message]) RecordExceptionFailure(ctx context.Context, maxAttempts int, exception *ClaimedException, failureErr error, disableDeliveryLog bool) error {
	return d.DatastoreRetry.Wrap(ctx, func() error {
		return d.recordExceptionFailure(ctx, maxAttempts, exception, failureErr, disableDeliveryLog)
	})
}

func (d *ConsumerDatastore[Message]) recordExceptionFailure(ctx context.Context, maxAttempts int, exception *ClaimedException, failureErr error, disableDeliveryLog bool) error {
	if exception.Attempts >= maxAttempts {
		var sql string
		if disableDeliveryLog {
			sql = fmt.Sprintf(`
				UPDATE %s
				SET
					status = 'dead',
					lease_token = NULL,
					lease_until = NULL,
					last_error = $3,
					updated_at = now()
				WHERE consumer_group = $1
					AND message_id = $2
					AND lease_token = $4;
			`, topic.DeliveryTable(exception.TopicID))
		} else {
			// updated CTE + INSERT ... WHERE EXISTS keeps the UPDATE and its
			// delivery_log_<topic_id> row atomic
			sql = fmt.Sprintf(`
				WITH updated AS (
					UPDATE %[1]s
					SET
						status = 'dead',
						lease_token = NULL,
						lease_until = NULL,
						last_error = $3,
						updated_at = now()
					WHERE consumer_group = $1
						AND message_id = $2
						AND lease_token = $4
					RETURNING 1
				)
				INSERT INTO %[2]s (consumer_group, message_id, attempt, error)
				SELECT $1, $2, $5, $3
				WHERE EXISTS (SELECT 1 FROM updated);
			`, topic.DeliveryTable(exception.TopicID), topic.DeliveryLogTable(exception.TopicID))
		}

		args := []any{exception.ConsumerGroup, exception.MessageId, failureErr.Error(), exception.LeaseToken}
		if !disableDeliveryLog {
			args = append(args, exception.Attempts)
		}
		tag, err := d.Datastore.Pool.Exec(ctx, sql, args...)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrLeaseLost
		}

		d.Logger.WarnContext(ctx, "exception dead-lettered after max attempts", "group", exception.ConsumerGroup, "topic_id", exception.TopicID, "message_id", exception.MessageId, "attempts", exception.Attempts)
		return nil
	}

	// clears the lease so it's claimable as a fresh 'ready' retry once can_run_after passes.
	var sql string
	if disableDeliveryLog {
		sql = fmt.Sprintf(`
			UPDATE %s
			SET
				status = 'ready',
				lease_token = NULL,
				lease_until = NULL,
				last_error = $3,
				can_run_after = now() + make_interval(secs => $4),
				updated_at = now()
			WHERE consumer_group = $1
				AND message_id = $2
				AND lease_token = $5;
		`, topic.DeliveryTable(exception.TopicID))
	} else {
		sql = fmt.Sprintf(`
			WITH updated AS (
				UPDATE %[1]s
				SET
					status = 'ready',
					lease_token = NULL,
					lease_until = NULL,
					last_error = $3,
					can_run_after = now() + make_interval(secs => $4),
					updated_at = now()
				WHERE consumer_group = $1
					AND message_id = $2
					AND lease_token = $5
				RETURNING 1
			)
			INSERT INTO %[2]s (consumer_group, message_id, attempt, error)
			SELECT $1, $2, $6, $3
			WHERE EXISTS (SELECT 1 FROM updated);
		`, topic.DeliveryTable(exception.TopicID), topic.DeliveryLogTable(exception.TopicID))
	}

	args := []any{exception.ConsumerGroup, exception.MessageId, failureErr.Error(), d.MessageRetry.CalculateDelay(exception.Attempts - 1).Seconds(), exception.LeaseToken}
	if !disableDeliveryLog {
		args = append(args, exception.Attempts)
	}
	tag, err := d.Datastore.Pool.Exec(ctx, sql, args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrLeaseLost
	}

	return nil
}

// execBatch sends every queued statement as one pipelined round trip instead
// of N sequential ones, surfacing the first failure
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

// func (d *ConsumerDatastore[Message]) ClaimMessages(ctx context.Context, limit int, leaseDuration time.Duration) ([]MessageRow, error) {
// 	// FOR UPDATE SKIP LOCKED makes the sub select query safe for update ie other consumers in group cannot select while being updated:
// 	sql := `
// 		UPDATE message_log
// 		SET
// 			status = 'processing',
// 			lease_until = now() + make_interval(secs => $2),
// 			lease_token = gen_random_uuid(), -- 'owner' claims this uuid
// 			attempts = attempts + 1
// 		WHERE id IN (
// 			SELECT id FROM message_log
// 			WHERE (status = 'ready' AND can_run_after <= now())
// 				OR (status = 'processing' AND lease_until < now()) -- retreive any 'expired' work
// 			ORDER BY id
// 			LIMIT $1
// 			FOR UPDATE SKIP LOCKED
// 		)
// 		RETURNING *;
// 	`

// 	rows, err := d.Datastore.Pool.Query(ctx, sql, limit, leaseDuration.Seconds())
// 	if err != nil {
// 		return nil, err
// 	}

// 	return pgx.CollectRows(rows, pgx.RowToStructByName[MessageRow])
// }

// func (d *ConsumerDatastore[Message]) ForceReclaim(ctx context.Context, messageRow *MessageRow) error {
// 	ctx, cancel := context.WithDeadline(context.WithoutCancel(ctx), *messageRow.LeaseUntil)
// 	defer cancel()

// 	sql := `
// 		UPDATE message_log
// 		SET
// 			status = 'ready',
// 			lease_until = NULL,
// 			lease_token = NULL,
// 			last_error = NULL,
// 			attempts = attempts - 1,    -- undo the claim-time increment; this was never run
// 			can_run_after = now()
// 		WHERE id = $1
// 			AND lease_token = $2;
// 	`

// 	_, err := d.Datastore.Pool.Exec(ctx, sql, messageRow.Id, messageRow.LeaseToken)
// 	if err != nil {
// 		return err
// 	}

// 	return nil
// }

// func (d *ConsumerDatastore[Message]) RecordSuccess(ctx context.Context, messageRow *MessageRow) error {
// 	ctx, cancel := context.WithDeadline(context.WithoutCancel(ctx), *messageRow.LeaseUntil)
// 	defer cancel()

// 	sql := `
// 		UPDATE message_log
// 		SET
// 			status = 'done',
// 			lease_until = NULL,
// 			lease_token = NULL,
// 			last_error = NULL
// 		WHERE id = $1
// 			AND lease_token = $2;
// 	`

// 	tags, err := d.Datastore.Pool.Exec(ctx, sql, messageRow.Id, messageRow.LeaseToken)
// 	if err != nil {
// 		return err
// 	}
// 	// this should only happen if the lease_token no longer is valid
// 	// ie it was claimed by another worker or said another way the leased expired
// 	if tags.RowsAffected() == 0 {
// 		return ErrLeaseLost
// 	}

// 	return nil
// }

// func (d *ConsumerDatastore[Message]) RecordFailure(ctx context.Context, maxAttempts int, messageRow *MessageRow, failureErr error) error {
// 	ctx, cancel := context.WithDeadline(context.WithoutCancel(ctx), *messageRow.LeaseUntil)
// 	defer cancel()

// 	if messageRow.Attempts >= maxAttempts {
// 		// terminal failure, no more retries
// 		return d.RecordTerminal(ctx, messageRow, failureErr)
// 	} else {
// 		// non terminal failure, should retry after backoff
// 		sql := `
// 			UPDATE message_log
// 			SET
// 				status = 'ready',
// 				lease_until = NULL,
// 				lease_token = NULL,
// 				last_error = $3,
// 				can_run_after = now() + make_interval(secs => $4)
// 			WHERE id = $1
// 				AND lease_token = $2;
// 		`

// 		tags, err := d.Datastore.Pool.Exec(ctx, sql, messageRow.Id, messageRow.LeaseToken, failureErr.Error(), backoff(messageRow.Attempts).Seconds())
// 		if err != nil {
// 			return err
// 		}
// 		// this should only happen if the lease_token no longer is valid
// 		// ie it was claimed by another worker or said another way the leased expired
// 		if tags.RowsAffected() == 0 {
// 			return ErrLeaseLost
// 		}
// 		return nil
// 	}
// }

// func (d *ConsumerDatastore[Message]) RecordTerminal(ctx context.Context, messageRow *MessageRow, terminalErr error) error {
// 	ctx, cancel := context.WithDeadline(context.WithoutCancel(ctx), *messageRow.LeaseUntil)
// 	defer cancel()

// 	// terminal failure, no more retries
// 	sql := `
// 		UPDATE message_log
// 		SET
// 			status = 'dead',
// 			lease_until = NULL,
// 			lease_token = NULL,
// 			last_error = $3
// 		WHERE id = $1
// 			AND lease_token = $2;
// 	`

// 	tags, err := d.Datastore.Pool.Exec(ctx, sql, messageRow.Id, messageRow.LeaseToken, terminalErr.Error())
// 	if err != nil {
// 		return err
// 	}
// 	// this should only happen if the lease_token no longer is valid
// 	// ie it was claimed by another worker or said another way the leased expired
// 	if tags.RowsAffected() == 0 {
// 		return ErrLeaseLost
// 	}
// 	return nil
// }
