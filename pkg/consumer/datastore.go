package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// TODO - just convert WorkType to Message I like it better more generic
// specifically converting WorkType to Message here to be more in line with community standards

var (
	ErrLeaseLost = errors.New("lease lost: row reclaimed by another consumer")
)

type Datastore[Message any] interface {
	UpsertCursor(ctx context.Context, topicID int64, consumerGroup string) error
	EnsureNextPartition(ctx context.Context, topicID int64, partitionSize int64, safetyBuffer int64) error
	DropExpiredPartitions(ctx context.Context, topicID int64, partitionSize int64, ttl time.Duration, allowDropPastCommitted bool) error
	SweepExpiredPartitions(ctx context.Context, topicID int64, partitionSize int64, ttl time.Duration, allowDropPastCommitted bool, batchSize int) error
	// Commit frees the range's lease, then parks any failures as sparse deliveries rows.
	Commit(ctx context.Context, topicID int64, consumerGroup string, token pgtype.UUID, exceptions []MessageException, terminals []MessageTerminal) error
	AdvanceWaterline(ctx context.Context, topicID int64, consumerGroup string) (int64, error)
	FanOut(ctx context.Context, topicID int64, consumerGroup string) error
	// ClaimMessages(ctx context.Context, batchLimit int, processingTimeout time.Duration) ([]MessageRow, error)
	ClaimMessagesWithCursor(ctx context.Context, topicID int64, consumerGroup string, limit, maxRangeReclaims int, leaseDuration time.Duration) (*ClaimedRange, error)
	ClaimMessagesWithLifecycle(ctx context.Context, topicID int64, consumerGroup string, limit int) ([]DeliveryRow, error)
	ClaimExceptions(ctx context.Context, topicID int64, consumerGroup string, limit, maxAttempts int, leaseDuration time.Duration) ([]ClaimedException, error)
	RecordExceptionSuccess(ctx context.Context, exception *ClaimedException) error
	RecordExceptionFailure(ctx context.Context, maxAttempts int, exception *ClaimedException, failureErr error) error
	// ForceReclaim(ctx context.Context, messageRow *MessageRow) error
	RecordSuccess(ctx context.Context, delivery *DeliveryRow) error
	RecordFailure(ctx context.Context, maxAttempts int, delivery *DeliveryRow, failureErr error) error
	RecordTerminal(ctx context.Context, delivery *DeliveryRow, failureErr error) error
	Shutdown(ctx context.Context) error
}

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

// DeliveryRow is one (consumer_group, topic_id, message_id) row of the deliveries
// table: the mutable per-consumer lifecycle state that lives off the immutable
// message_log. Payload is not stored on the row -- it's joined back in from
// message_log at claim time. Phase 6 skips the lease columns (lease_until /
// lease_token); crash recovery is Phase 6.5.
type DeliveryRow struct {
	ConsumerGroup string          `db:"consumer_group"`
	TopicID       int64           `db:"topic_id"`
	MessageId     int64           `db:"message_id"`
	Payload       json.RawMessage `db:"payload"`
	Status        string          `db:"status"`
	Attempts      int             `db:"attempts"`
}

type consumerDatastore[Message any] struct {
	Datastore *datastore.PostgresDatastore
}

func NewConsumerDatastore[Message any](ds *datastore.PostgresDatastore) *consumerDatastore[Message] {
	return &consumerDatastore[Message]{
		Datastore: ds,
	}
}

// logTable is topicID's own physical message log.
func logTable(topicID int64) string {
	return fmt.Sprintf("message_log_%d", topicID)
}

// partitionTable is logTable's nth partition -- message_log_<topic_id>_<n>.
func partitionTable(topicID, n int64) string {
	return fmt.Sprintf("%s_%d", logTable(topicID), n)
}

func (d *consumerDatastore[Message]) UpsertCursor(ctx context.Context, topicID int64, consumerGroup string) error {
	sql := `
		INSERT INTO cursors (consumer_group, topic_id)
		VALUES ($1, $2)
		ON CONFLICT DO NOTHING;
	`

	_, err := d.Datastore.Pool.Exec(ctx, sql, consumerGroup, topicID)
	if err != nil {
		return err
	}

	return nil
}

func (d *consumerDatastore[Message]) EnsureNextPartition(ctx context.Context, topicID int64, partitionSize int64, safetyBuffer int64) error {
	headSql := fmt.Sprintf(`
		SELECT COALESCE(MAX(id), 0) FROM %s;
	`, logTable(topicID))

	var head int64
	if err := d.Datastore.Pool.QueryRow(ctx, headSql).Scan(&head); err != nil {
		return err
	}

	roomInPartition := partitionSize - (head % partitionSize)

	// there is enough room in the current active partition
	if roomInPartition > safetyBuffer {
		return nil
	}

	nextPartition := head/partitionSize + 1

	createPartitionSql := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s
			PARTITION OF %s
			FOR VALUES FROM (%d) TO (%d);
	`, partitionTable(topicID, nextPartition), logTable(topicID), nextPartition*partitionSize, (nextPartition+1)*partitionSize)

	_, err := d.Datastore.Pool.Exec(ctx, createPartitionSql)
	if err != nil {
		// IF NOT EXISTS still races -- losing to a concurrent creator means it exists
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "42P07" {
			return nil
		}
		return err
	}

	return nil
}

// DropExpiredPartitions drops each surviving partition whose newest row is
// past ttl, skipping the active partition and (unless overridden) anything a
// lagging group hasn't committed past yet.
func (d *consumerDatastore[Message]) DropExpiredPartitions(ctx context.Context, topicID int64, partitionSize int64, ttl time.Duration, allowDropPastCommitted bool) error {
	if ttl <= 0 {
		return nil // retention disabled - partitions kept forever
	}

	headSql := fmt.Sprintf(`
		SELECT COALESCE(MAX(id), 0) FROM %s;
	`, logTable(topicID))
	var head int64
	if err := d.Datastore.Pool.QueryRow(ctx, headSql).Scan(&head); err != nil {
		return err
	}
	activePartition := head / partitionSize

	partitions, err := d.existingPartitions(ctx, topicID)
	if err != nil {
		return err
	}

	floor, err := d.cursorFloor(ctx, topicID)
	if err != nil {
		return err
	}

	for _, n := range partitions {
		if n >= activePartition {
			continue // never touch the active partition, or anything at/after it
		}

		expired, err := d.partitionExpired(ctx, topicID, n, ttl)
		if err != nil {
			return err
		}
		if !expired {
			continue // not this partition's turn yet -- each partition is judged independently
		}

		lastIdInPartition := (n+1)*partitionSize - 1
		if !allowDropPastCommitted && floor != nil && lastIdInPartition > *floor {
			continue // a lagging group hasn't resolved this range yet
		}

		if err := d.dropPartition(ctx, topicID, n, partitionSize); err != nil {
			return err
		}
	}

	return nil
}

// existingPartitions lists surviving message_log_<topic_id>_<n> partition numbers.
func (d *consumerDatastore[Message]) existingPartitions(ctx context.Context, topicID int64) ([]int64, error) {
	sql := fmt.Sprintf(`
		SELECT REPLACE(c.relname, '%s_', '')::bigint AS n
		FROM pg_inherits i
		JOIN pg_class c ON c.oid = i.inhrelid
		WHERE i.inhparent = '%s'::regclass;
	`, logTable(topicID), logTable(topicID))

	rows, err := d.Datastore.Pool.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var partitions []int64
	for rows.Next() {
		var n int64
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}

		partitions = append(partitions, n)
	}

	return partitions, rows.Err()
}

// cursorFloor is the waterline floor: the most-lagging group's committed
// offset within this topic (nil if none exist yet). Scoped to topic_id so a
// lagging group on another topic can't block this topic's drops/sweeps.
func (d *consumerDatastore[Message]) cursorFloor(ctx context.Context, topicID int64) (*int64, error) {
	sql := `
		SELECT MIN(committed) FROM cursors WHERE topic_id = $1;
	`

	var floor *int64
	err := d.Datastore.Pool.QueryRow(ctx, sql, topicID).Scan(&floor)
	return floor, err
}

// partitionExpired reports whether a partition's newest row is past ttl.
func (d *consumerDatastore[Message]) partitionExpired(ctx context.Context, topicID int64, n int64, ttl time.Duration) (bool, error) {
	sql := fmt.Sprintf(`
		SELECT created_at FROM %s
		ORDER BY id DESC -- rides the PK index; id order approx time order, no created_at index needed
		LIMIT 1;
	`, partitionTable(topicID, n))

	var newest time.Time
	err := d.Datastore.Pool.QueryRow(ctx, sql).Scan(&newest)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil // empty -- nothing to judge, so not expired
	}
	if err != nil {
		return false, err
	}

	return time.Since(newest) >= ttl, nil
}

// SweepExpiredPartitions drains the ttl-expired prefix of every surviving
// partition -- covers the low-volume tail that never fills a partition wide
// enough to earn a whole-partition drop.
func (d *consumerDatastore[Message]) SweepExpiredPartitions(ctx context.Context, topicID int64, partitionSize int64, ttl time.Duration, allowDropPastCommitted bool, batchSize int) error {
	if ttl <= 0 {
		return nil // retention disabled
	}

	partitions, err := d.existingPartitions(ctx, topicID)
	if err != nil {
		return err
	}

	floor, err := d.cursorFloor(ctx, topicID)
	if err != nil {
		return err
	}
	if allowDropPastCommitted {
		floor = nil
	}

	cutoff := time.Now().Add(-ttl)

	// caps a full drain to break any potential infinite loops
	maxBatches := int((partitionSize + int64(batchSize) - 1) / int64(batchSize))

	for _, n := range partitions { // every partition, independently -- one backlog can't block the rest
		for range maxBatches {
			swept, err := d.sweepBatch(ctx, topicID, n, cutoff, floor, batchSize)
			if err != nil {
				return err
			}
			if swept < batchSize {
				break // ran out of expired rows (or hit the floor)
			}
		}
	}

	return nil
}

// sweepBatch deletes up to batchSize expired rows from the front of partition n,
// plus their orphaned deliveries rows, in one transaction.
func (d *consumerDatastore[Message]) sweepBatch(ctx context.Context, topicID int64, n int64, cutoff time.Time, floor *int64, batchSize int) (int, error) {
	tx, err := d.Datastore.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	sweepSql := fmt.Sprintf(`
		DELETE FROM %s
		WHERE id IN (
			SELECT id FROM %s
			WHERE created_at < $1
				AND ($3::bigint IS NULL OR id <= $3) -- nil floor (allowDropPastCommitted) skips the check
			ORDER BY id ASC -- walk the expired prefix from the front, same PK-index ride as partitionExpired
			LIMIT $2
		)
		RETURNING id;
	`, partitionTable(topicID, n), partitionTable(topicID, n))

	rows, err := tx.Query(ctx, sweepSql, cutoff, batchSize, floor)
	if err != nil {
		return 0, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return 0, err
	}

	if len(ids) > 0 {
		// otherwise these deliveries rows (mostly 'dead' DLQ) would join to nothing and park forever
		orphanSql := `
			DELETE FROM deliveries
			WHERE message_id = ANY($1);
		`
		if _, err := tx.Exec(ctx, orphanSql, ids); err != nil {
			return 0, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}

	return len(ids), nil
}

// dropPartition removes the partition and its deliveries rows in one transaction.
func (d *consumerDatastore[Message]) dropPartition(ctx context.Context, topicID int64, n int64, partitionSize int64) error {
	tx, err := d.Datastore.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	low := n * partitionSize
	high := (n + 1) * partitionSize

	// otherwise these deliveries rows (mostly 'dead' DLQ, since live ones are
	// already floor-protected) would join to nothing and park forever
	orphanSql := `
		DELETE FROM deliveries
		WHERE message_id >= $1
			AND message_id < $2;
	`
	if _, err := tx.Exec(ctx, orphanSql, low, high); err != nil {
		return err
	}

	dropSql := fmt.Sprintf(`
		DROP TABLE %s;
	`, partitionTable(topicID, n))

	if _, err := tx.Exec(ctx, dropSql); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// frees the lease FIRST, token-guarded -- so a reclaimed worker's stale commit
// bails before parking any phantom exception rows.
func (d *consumerDatastore[Message]) Commit(ctx context.Context, topicID int64, consumerGroup string, token pgtype.UUID, exceptions []MessageException, terminals []MessageTerminal) error {
	tx, err := d.Datastore.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// topic_id isn't load-bearing here -- token alone already disambiguates a
	// lease row -- but every leases query stays topic-scoped by convention.
	freeSql := `
		DELETE FROM leases
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
	// returns before ever parking.
	parkSql := `
		INSERT INTO deliveries (consumer_group, topic_id, message_id, status, attempts, can_run_after, last_error)
		VALUES (
			$1,
			$2,
			$3,
			$4,
			0,
			now() + interval '5 seconds',
			$5
		);
	`

	for _, e := range exceptions {
		if _, err := tx.Exec(ctx, parkSql, consumerGroup, topicID, e.MessageId, "ready", e.Err); err != nil {
			return err
		}
	}
	for _, t := range terminals {
		if _, err := tx.Exec(ctx, parkSql, consumerGroup, topicID, t.MessageId, "dead", t.Err); err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

// committed is the waterline: the mark below which every offset is resolved. it
// rides at the lowest open lease's low, or at `claimed` when nothing is leased.
//
// Race Condition:
//
//	Because our claiming transaction in FreshClaimMessagesWithCursor advances
//	cursors.claimed AND inserts a lease we must read both cursors and leases
//	tables back in one SELECT, then write separately. Read them inside a single
//	UPDATE instead and postgres hands back the new claimed row but not the new
//	lease, so committed can advance past a range that is still being processed.
//
//	This is due to READ COMMITTED: an UPDATE re-reads the row it modifies at its
//	newest version, but its subqueries keep the snapshot from when the statement
//	began -- so cursors comes back fresh, leases stale.
func (d *consumerDatastore[Message]) AdvanceWaterline(ctx context.Context, topicID int64, consumerGroup string) (int64, error) {
	// 1. compute the advance target, LEAST of:
	// 		earliest open lease
	// 		earliest unresolved exception (dead doesn't count -- only ready/inflight block)
	// 		claimed (its caught up to head of log)
	// LEAST ignores NULLs so any/all of those can be absent.
	const targetSql = `
		SELECT LEAST(
			(SELECT MIN(low) FROM leases WHERE consumer_group = $1 AND topic_id = $2),
			(SELECT MIN(message_id) - 1 FROM deliveries WHERE consumer_group = $1 AND topic_id = $2 AND status IN ('ready', 'inflight')),
			claimed
		)
		FROM cursors
		WHERE consumer_group = $1 AND topic_id = $2;
	`

	var target int64
	if err := d.Datastore.Pool.QueryRow(ctx, targetSql, consumerGroup, topicID).Scan(&target); err != nil {
		return 0, err
	}

	// 2. apply it. GREATEST -> committed only ever moves forward.
	const rollSql = `
		UPDATE cursors
		SET committed = GREATEST(committed, $3)
		WHERE consumer_group = $1 AND topic_id = $2
		RETURNING committed;
	`

	var committed int64
	err := d.Datastore.Pool.QueryRow(ctx, rollSql, consumerGroup, topicID, target).Scan(&committed)
	return committed, err
}

// FanOut materializes one deliveries row per message this group is bound to receive.
func (d *consumerDatastore[Message]) FanOut(ctx context.Context, topicID int64, consumerGroup string) error {
	sql := fmt.Sprintf(`
		INSERT INTO deliveries (consumer_group, topic_id, message_id, status)
		SELECT
			$1,
			$2,
			m.id,
			'ready'
		FROM %s m -- no need to batch / limit this is for demonstration purposes only
		WHERE (
			-- no bindings for (consumer_group, topic_id) exists
			NOT EXISTS (
				SELECT 1 FROM bindings b
				WHERE b.consumer_group = $1 AND b.topic_id = $2
			)
			-- bindings for (consumer_group, topic_id) exists and match routing_key pattern
			OR EXISTS (
				SELECT 1 FROM bindings b
				WHERE b.consumer_group = $1 AND b.topic_id = $2
					AND m.routing_key ~ b.pattern
			)
			-- if bindings exist but our routing_key does not match any of them
			-- no row is materialized for this message at all
		)
		AND (
			-- unkeyed rows are never compacted
			m.compaction_key IS NULL
			-- keyed rows materialize a delivery only if nothing newer exists for
			-- the same key
			OR NOT EXISTS (
				SELECT 1 FROM %s newer
				WHERE newer.compaction_key = m.compaction_key
					AND newer.id > m.id
			)
		)
		ON CONFLICT DO NOTHING;
	`, logTable(topicID), logTable(topicID))

	_, err := d.Datastore.Pool.Exec(ctx, sql, consumerGroup, topicID)
	if err != nil {
		return err
	}

	return nil
}

// try to pick up a crashed range (an expired lease) and only claims fresh work
// from the frontier if there's nothing to reclaim -- so crashed ranges drain first.
func (d *consumerDatastore[Message]) ClaimMessagesWithCursor(ctx context.Context, topicID int64, consumerGroup string, limit, maxRangeReclaims int, leaseDuration time.Duration) (*ClaimedRange, error) {
	reclaimed, err := d.ReclaimWithCursor(ctx, topicID, consumerGroup, limit, maxRangeReclaims, leaseDuration)
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
func (d *consumerDatastore[Message]) ReclaimWithCursor(ctx context.Context, topicID int64, consumerGroup string, limit, maxRangeReclaims int, leaseDuration time.Duration) (*ClaimedRange, error) {
	tx, err := d.Datastore.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	// a single in-place UPDATE, not delete+insert -- reclaims accumulates on the
	// SAME row instead of resetting to 0 every time. token still rotates, so a
	// dead worker's stale commit still no-ops the same as before.
	reclaimSql := `
		UPDATE leases
		SET
			reclaims = reclaims + 1,
			until = now() + make_interval(secs => $2),
			token = gen_random_uuid()
		WHERE (token, consumer_group) IN (
			SELECT token, consumer_group FROM leases
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

	if lease.Reclaims >= maxRangeReclaims {
		if err := d.quarantine(ctx, tx, consumerGroup, lease); err != nil {
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
// separate retry budget from the range's now-exhausted reclaim count) and the
// lease frees for good. From here each message lives or dies on its own via the
// exact same exception-window machinery as an ordinary CursorClaim failure --
// AdvanceWaterline's exception-blocker term pins committed on whichever
// resolves last, so one bad message no longer holds up its siblings forever.
func (d *consumerDatastore[Message]) quarantine(ctx context.Context, tx pgx.Tx, consumerGroup string, lease LeaseRow) error {
	parkSql := fmt.Sprintf(`
		INSERT INTO deliveries (consumer_group, message_id, status, attempts, last_error)
		SELECT $1, id, 'ready', 0, 'quarantined: range reclaimed too many times'
		FROM %s
		WHERE id > $2
			AND id <= $3;
	`, logTable(lease.TopicID))
	if _, err := tx.Exec(ctx, parkSql, consumerGroup, lease.Low, lease.High); err != nil {
		return err
	}

	freeSql := `
		DELETE FROM leases
		WHERE consumer_group = $1
			AND token = $2
			AND topic_id = $3;
	`
	_, err := tx.Exec(ctx, freeSql, consumerGroup, lease.Token, lease.TopicID)
	return err
}

// readMessages reads topicID's message_log rows in (low, high], ordered by id.
func (d *consumerDatastore[Message]) readMessages(ctx context.Context, tx pgx.Tx, topicID int64, consumerGroup string, low, high int64) ([]MessageRow, error) {
	sql := fmt.Sprintf(`
		SELECT m.id, m.payload, m.created_at FROM %s m
		WHERE m.id > $1
			AND m.id <= $2
			AND (
				-- no bindings for consumer_group exists
				NOT EXISTS (
					SELECT 1 FROM bindings b
					WHERE b.consumer_group = $3
				)
				-- bindings for consumer_group exists and match routing_key pattern
				OR EXISTS (
					SELECT 1 FROM bindings b
					WHERE b.consumer_group = $3
						AND m.routing_key ~ b.pattern
				)
				-- if bindings exist but our routing_key does not match any of them
				-- we do not return anything
			)
			AND (
				-- unkeyed rows are never compacted
				m.compaction_key IS NULL
				-- keyed rows are eligible only if nothing newer exists for the same
				-- key -- unbounded on purpose: what this owes is at-least-once
				-- delivery of a key's CURRENT latest value, not of every version
				-- ever written, so a superseded row can be dropped even outside
				-- this claim's own (low, high]
				OR NOT EXISTS (
					SELECT 1 FROM %s newer
					WHERE newer.compaction_key = m.compaction_key
						AND newer.id > m.id
				)
			)
		-- rows MUST come back in id order or a batch LIMIT could
		-- return an arbitrary subset and the cursor would advance past unread offsets
		ORDER BY m.id;
	`, logTable(topicID), logTable(topicID))

	rows, err := tx.Query(ctx, sql, low, high, consumerGroup)
	if err != nil {
		return nil, err
	}

	return pgx.CollectRows(rows, pgx.RowToStructByName[MessageRow])
}

func (d *consumerDatastore[Message]) FreshClaimMessagesWithCursor(ctx context.Context, topicID int64, consumerGroup string, limit int, leaseDuration time.Duration) (*ClaimedRange, error) {
	tx, err := d.Datastore.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	// TODO - projector could likely tracked head in a RWMutex such that it doesn't need to be calculated here
	// TODO - consider if we should lock any rows like claimed cursor during this tx
	cursorSql := fmt.Sprintf(`
		WITH old_values AS ( -- PG18+ has old / new syntax in returning but we want older version compatibility so use CTE
			SELECT * FROM cursors
			WHERE consumer_group = $1 AND topic_id = $3
		)
		UPDATE cursors
		SET
			claimed = LEAST(cursors.claimed + $2, (SELECT MAX(id) FROM %s)) -- move claimed frontier forward by max(batchLimit)
		FROM old_values
		WHERE cursors.consumer_group = $1
			AND cursors.topic_id = $3
			AND cursors.claimed < (SELECT MAX(id) FROM %s)
		RETURNING
			old_values.claimed AS low,
			cursors.claimed AS high;
	`, logTable(topicID), logTable(topicID))

	cursorRows, err := tx.Query(ctx, cursorSql, consumerGroup, limit, topicID)
	if err != nil {
		return nil, err
	}

	claimedRange, err := pgx.CollectOneRow(cursorRows, pgx.RowToStructByName[CursorRange])
	if err != nil {
		// two cases for no rows returned
		// 1. We are at head of message_log ie not messages to process
		// 2. We could not find the cursor for this consumer group
		// TODO - for now we just consider 1 but should have better validation for 2 edge case
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}

		return nil, err
	}

	return d.ClaimMessages(
		ctx, tx, topicID, consumerGroup, claimedRange.Low, claimedRange.High, limit, leaseDuration,
	)
}

func (d *consumerDatastore[Message]) ClaimMessages(
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
		INSERT INTO leases (consumer_group, topic_id, low, high, until)
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

func (d *consumerDatastore[Message]) ClaimMessagesWithLifecycle(ctx context.Context, topicID int64, consumerGroup string, limit int) ([]DeliveryRow, error) {
	// Claim this group's own delivery rows and move them 'ready' -> 'processing' in
	// one statement (the Phase 2 state machine, now per-(group, topic, message) instead
	// of per-message). SKIP LOCKED keeps competing workers from grabbing the same row.
	//
	// deliveries only stores message_id, not the payload, so we join this topic's
	// message_log back in -- the log stays immutable, all mutation lives in deliveries.
	//
	// Phase 6 deliberately has no lease: a 'processing' row that never gets resolved
	// (consumer crash) just sits there. Visibility-timeout reclaim is Phase 6.5.
	sql := fmt.Sprintf(`
		WITH claimed AS (
			UPDATE deliveries
			SET
				status = 'processing',
				attempts = attempts + 1,
				updated_at = now()
			WHERE (consumer_group, topic_id, message_id) IN (
				SELECT consumer_group, topic_id, message_id FROM deliveries
				WHERE consumer_group = $1
					AND topic_id = $3
					AND status = 'ready'
				ORDER BY message_id
				LIMIT $2
				FOR UPDATE SKIP LOCKED
			)
			RETURNING consumer_group, topic_id, message_id, status, attempts
		)
		SELECT
			c.consumer_group,
			c.topic_id,
			c.message_id,
			c.status,
			c.attempts,
			m.payload
		FROM claimed c
		JOIN %s m ON m.id = c.message_id
		ORDER BY c.message_id;
	`, logTable(topicID))

	rows, err := d.Datastore.Pool.Query(ctx, sql, consumerGroup, limit, topicID)
	if err != nil {
		return nil, err
	}

	return pgx.CollectRows(rows, pgx.RowToStructByName[DeliveryRow])
}

// ClaimExceptions drains the sparse exception window: kill exhausted deliveries, then claim.
func (d *consumerDatastore[Message]) ClaimExceptions(ctx context.Context, topicID int64, consumerGroup string, limit, maxAttempts int, leaseDuration time.Duration) ([]ClaimedException, error) {
	// an exception that causes a crash loop never resolves normally -- without this
	// backstop it would reclaim forever, pinning committed below it forever.
	killSql := `
		UPDATE deliveries
		SET
			status = 'dead',
			lease_token = NULL,
			lease_until = NULL,
			updated_at = now(),
			last_error = concat(last_error, ' [killed: crash-loop hit max attempts]')
		WHERE consumer_group = $1
			AND topic_id = $3
			AND status = 'inflight'
			AND lease_until < now()
			AND attempts >= $2;
	`
	if _, err := d.Datastore.Pool.Exec(ctx, killSql, consumerGroup, maxAttempts, topicID); err != nil {
		return nil, err
	}

	// joins to this topic's own message_log, since deliveries stores no payload of its own.
	claimSql := fmt.Sprintf(`
		WITH claimed AS (
			UPDATE deliveries
			SET
				status = 'inflight',
				lease_token = gen_random_uuid(),
				lease_until = now() + make_interval(secs => $3),
				attempts = attempts + 1,
				updated_at = now()
			WHERE (consumer_group, topic_id, message_id) IN
			(
				-- claim either:
				--   retryable 'ready' exceptions
				--   expired 'inflight' exceptions
				SELECT consumer_group, topic_id, message_id FROM deliveries
				WHERE consumer_group = $1
					AND topic_id = $4
					AND can_run_after <= now()
					AND (
						status = 'ready' OR
						(status = 'inflight' AND lease_until < now())
					)
				ORDER BY message_id
				LIMIT $2
				FOR UPDATE SKIP LOCKED
			)
			RETURNING consumer_group, topic_id, message_id, attempts, lease_token
		)
		SELECT
			c.consumer_group,
			c.topic_id,
			c.message_id,
			c.attempts,
			c.lease_token,
			m.payload
		FROM claimed c
		JOIN %s m ON m.id = c.message_id
		ORDER BY c.message_id;
	`, logTable(topicID))

	rows, err := d.Datastore.Pool.Query(ctx, claimSql, consumerGroup, limit, leaseDuration.Seconds(), topicID)
	if err != nil {
		return nil, err
	}

	return pgx.CollectRows(rows, pgx.RowToStructByName[ClaimedException])
}

// success pop-deletes the row -- same sparse convention as never parking it: no row means resolved.
func (d *consumerDatastore[Message]) RecordExceptionSuccess(ctx context.Context, exception *ClaimedException) error {
	sql := `
		DELETE FROM deliveries
		WHERE consumer_group = $1
			AND topic_id = $2
			AND message_id = $3
			AND lease_token = $4;
	`

	tag, err := d.Datastore.Pool.Exec(ctx, sql, exception.ConsumerGroup, exception.TopicID, exception.MessageId, exception.LeaseToken)
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
func (d *consumerDatastore[Message]) RecordExceptionFailure(ctx context.Context, maxAttempts int, exception *ClaimedException, failureErr error) error {
	if exception.Attempts >= maxAttempts {
		sql := `
			UPDATE deliveries
			SET
				status = 'dead',
				lease_token = NULL,
				lease_until = NULL,
				last_error = $4,
				updated_at = now()
			WHERE consumer_group = $1
				AND topic_id = $2
				AND message_id = $3
				AND lease_token = $5;
		`

		tag, err := d.Datastore.Pool.Exec(ctx, sql, exception.ConsumerGroup, exception.TopicID, exception.MessageId, failureErr.Error(), exception.LeaseToken)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrLeaseLost
		}

		return nil
	}

	// clears the lease so it's claimable as a fresh 'ready' retry once can_run_after passes.
	sql := `
		UPDATE deliveries
		SET
			status = 'ready',
			lease_token = NULL,
			lease_until = NULL,
			last_error = $4,
			can_run_after = now() + make_interval(secs => $5),
			updated_at = now()
		WHERE consumer_group = $1
			AND topic_id = $2
			AND message_id = $3
			AND lease_token = $6;
	`

	tag, err := d.Datastore.Pool.Exec(ctx, sql, exception.ConsumerGroup, exception.TopicID, exception.MessageId, failureErr.Error(), backoff(exception.Attempts).Seconds(), exception.LeaseToken)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrLeaseLost
	}

	return nil
}

// RecordSuccess marks a claimed delivery 'done'. Terminal success for this
// (group, message); the log row is untouched and other groups are unaffected.
func (d *consumerDatastore[Message]) RecordSuccess(ctx context.Context, delivery *DeliveryRow) error {
	sql := `
		UPDATE deliveries
		SET
			status = 'done',
			last_error = NULL,
			updated_at = now()
		WHERE consumer_group = $1
			AND topic_id = $2
			AND message_id = $3;
	`

	_, err := d.Datastore.Pool.Exec(ctx, sql, delivery.ConsumerGroup, delivery.TopicID, delivery.MessageId)
	return err
}

// RecordFailure handles a processing error: retry until attempts are exhausted,
// then hand off to RecordTerminal (the per-group DLQ). attempts was already
// incremented at claim time, so >= maxAttempts means this was the last try.
// Phase 6 has no backoff (the deliveries table carries no can_run_after) -- a
// 'ready' row is simply re-claimed on the next poll.
func (d *consumerDatastore[Message]) RecordFailure(ctx context.Context, maxAttempts int, delivery *DeliveryRow, failureErr error) error {
	if delivery.Attempts >= maxAttempts {
		return d.RecordTerminal(ctx, delivery, failureErr)
	}

	sql := `
		UPDATE deliveries
		SET
			status = 'ready',
			last_error = $4,
			updated_at = now()
		WHERE consumer_group = $1
			AND topic_id = $2
			AND message_id = $3;
	`

	_, err := d.Datastore.Pool.Exec(ctx, sql, delivery.ConsumerGroup, delivery.TopicID, delivery.MessageId, failureErr.Error())
	return err
}

// RecordTerminal dead-letters a delivery: no more retries. The DLQ for a group is
// just `WHERE consumer_group = $1 AND status = 'dead'`; one group can dead-letter a
// message while another processes the same offset fine.
func (d *consumerDatastore[Message]) RecordTerminal(ctx context.Context, delivery *DeliveryRow, terminalErr error) error {
	sql := `
		UPDATE deliveries
		SET
			status = 'dead',
			last_error = $4,
			updated_at = now()
		WHERE consumer_group = $1
			AND topic_id = $2
			AND message_id = $3;
	`

	_, err := d.Datastore.Pool.Exec(ctx, sql, delivery.ConsumerGroup, delivery.TopicID, delivery.MessageId, terminalErr.Error())
	return err
}

// func (d *consumerDatastore[Message]) ClaimMessages(ctx context.Context, limit int, leaseDuration time.Duration) ([]MessageRow, error) {
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

// func (d *consumerDatastore[Message]) ForceReclaim(ctx context.Context, messageRow *MessageRow) error {
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

// func (d *consumerDatastore[Message]) RecordSuccess(ctx context.Context, messageRow *MessageRow) error {
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

// func (d *consumerDatastore[Message]) RecordFailure(ctx context.Context, maxAttempts int, messageRow *MessageRow, failureErr error) error {
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

// func (d *consumerDatastore[Message]) RecordTerminal(ctx context.Context, messageRow *MessageRow, terminalErr error) error {
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

// TODO - backoff should be overrideable func from consumer definition
func backoff(attempts int) time.Duration {
	// TODO - this should be config params
	max := 5 * time.Minute
	min := 1 * time.Second

	// attempt number validation
	if attempts < 1 {
		return min
	}
	if attempts > 100 {
		return max
	}

	// (attempt-1)^2 seconds, floored to min(1s)
	// attempt 1 => 1s, attempt 2 => 1s, attempt 3 => 4s, attempt 4 => 9s
	backoff := time.Second * time.Duration((attempts-1)*(attempts-1))
	// clamp to min and max
	if backoff < min {
		return min
	}
	if backoff > max {
		return max
	}
	return backoff
}

func (d *consumerDatastore[Message]) Shutdown(ctx context.Context) error {
	d.Datastore.Pool.Close()

	return nil
}
