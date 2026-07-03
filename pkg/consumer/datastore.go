package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TODO - just convert WorkType to Message I like it better more generic
// specifically converting WorkType to Message here to be more in line with community standards

var (
	ErrLeaseLost = errors.New("lease lost: row reclaimed by another consumer")
)

type Datastore[Message any] interface {
	UpsertCursor(ctx context.Context, consumerGroup string) error
	// Commit frees the range's lease, then parks any failures as sparse deliveries rows.
	Commit(ctx context.Context, consumerGroup string, token pgtype.UUID, exceptions []MessageException, terminals []MessageTerminal) error
	AdvanceWaterline(ctx context.Context, consumerGroup string) (int64, error)
	FanOut(ctx context.Context, consumerGroup string) error
	// ClaimMessages(ctx context.Context, batchLimit int, processingTimeout time.Duration) ([]MessageRow, error)
	ClaimMessagesWithCursor(ctx context.Context, consumerGroup string, limit int, leaseDuration time.Duration) (*ClaimedRange, error)
	ClaimMessagesWithLifecycle(ctx context.Context, consumerGroup string, limit int) ([]DeliveryRow, error)
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

// DeliveryRow is one (consumer_group, message_id) row of the deliveries table:
// the mutable per-consumer lifecycle state that lives off the immutable message_log.
// Payload is not stored on the row -- it's joined back in from message_log at claim time.
// Phase 6 skips the lease columns (lease_until / lease_token); crash recovery is Phase 6.5.
type DeliveryRow struct {
	ConsumerGroup string          `db:"consumer_group"`
	MessageId     int64           `db:"message_id"`
	Payload       json.RawMessage `db:"payload"`
	Status        string          `db:"status"`
	Attempts      int             `db:"attempts"`
}

type PostgresConnectionParams struct {
	User     string
	Pass     string
	Host     string
	Port     int
	Database string
	MaxConns int // optional; if > 0 sets pool_max_conns. default pgx pool is max(4, numCPU), which caps high worker counts.
}

type PostgresDatastore[Message any] struct {
	Pool *pgxpool.Pool
}

func NewPostgresDatastore[Message any](ctx context.Context, params *PostgresConnectionParams) (*PostgresDatastore[Message], error) {
	// TODO - params validate using go playground (maybe no lib)

	connectionString := fmt.Sprintf("postgres://%s:%s@%s:%s/%s",
		params.User, params.Pass, params.Host, strconv.Itoa(params.Port), params.Database,
	)
	if params.MaxConns > 0 {
		connectionString += fmt.Sprintf("?pool_max_conns=%d", params.MaxConns)
	}

	pool, err := pgxpool.New(ctx, connectionString)
	if err != nil {
		return nil, err
	}

	// Sanity check
	if err = pool.Ping(ctx); err != nil {
		return nil, err
	}

	return &PostgresDatastore[Message]{
		Pool: pool,
	}, nil
}

func (d *PostgresDatastore[Message]) UpsertCursor(ctx context.Context, consumerGroup string) error {
	sql := `
		INSERT INTO cursors (consumer_group)
		VALUES ($1)
		ON CONFLICT DO NOTHING;
	`

	_, err := d.Pool.Exec(ctx, sql, consumerGroup)
	if err != nil {
		return err
	}

	return nil
}

// frees the lease FIRST, token-guarded -- so a reclaimed worker's stale commit
// bails before parking any phantom exception rows.
func (d *PostgresDatastore[Message]) Commit(ctx context.Context, consumerGroup string, token pgtype.UUID, exceptions []MessageException, terminals []MessageTerminal) error {
	tx, err := d.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	freeSql := `
		DELETE FROM leases
		WHERE consumer_group = $1
			AND token = $2;
	`

	tag, err := tx.Exec(ctx, freeSql, consumerGroup, token)
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
		INSERT INTO deliveries (consumer_group, message_id, status, attempts, can_run_after, last_error)
		VALUES (
			$1,
			$2,
			$3,
			0,
			now() + interval '5 seconds',
			$4
		);
	`

	for _, e := range exceptions {
		if _, err := tx.Exec(ctx, parkSql, consumerGroup, e.MessageId, "ready", e.Err); err != nil {
			return err
		}
	}
	for _, t := range terminals {
		if _, err := tx.Exec(ctx, parkSql, consumerGroup, t.MessageId, "dead", t.Err); err != nil {
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
func (d *PostgresDatastore[Message]) AdvanceWaterline(ctx context.Context, consumerGroup string) (int64, error) {
	// 1. compute the target. LEAST ignores NULLs, so zero open leases -> claimed.
	const targetSql = `
		SELECT LEAST((SELECT MIN(low) FROM leases WHERE consumer_group = $1), claimed)
		FROM cursors
		WHERE consumer_group = $1;
	`

	var target int64
	if err := d.Pool.QueryRow(ctx, targetSql, consumerGroup).Scan(&target); err != nil {
		return 0, err
	}

	// 2. apply it. GREATEST -> committed only ever moves forward.
	const rollSql = `
		UPDATE cursors
		SET committed = GREATEST(committed, $2)
		WHERE consumer_group = $1
		RETURNING committed;
	`

	var committed int64
	err := d.Pool.QueryRow(ctx, rollSql, consumerGroup, target).Scan(&committed)
	return committed, err
}

func (d *PostgresDatastore[Message]) FanOut(ctx context.Context, consumerGroup string) error {
	sql := `
		INSERT INTO deliveries (consumer_group, message_id, status)
		SELECT
			$1, 
			id, 
			'ready'
		FROM message_log -- no need to batch / limit this is for demonstration purposes only
		ON CONFLICT DO NOTHING;
	`

	_, err := d.Pool.Exec(ctx, sql, consumerGroup)
	if err != nil {
		return err
	}

	return nil
}

// try to pick up a crashed range (an expired lease) and only claims fresh work
// from the frontier if there's nothing to reclaim -- so crashed ranges drain first.
func (d *PostgresDatastore[Message]) ClaimMessagesWithCursor(ctx context.Context, consumerGroup string, limit int, leaseDuration time.Duration) (*ClaimedRange, error) {
	reclaimed, err := d.ReclaimWithCursor(ctx, consumerGroup, limit, leaseDuration)
	if err != nil {
		return nil, err
	}
	if reclaimed != nil {
		return reclaimed, nil
	}

	// nothing to reclaim -> try standard fresh claim (nil when caught up)
	return d.FreshClaimMessagesWithCursor(ctx, consumerGroup, limit, leaseDuration)
}

// grab ONE expired lease and re-reads its exact range so a
// worker that crashed mid-range doesn't strand those offsets.
func (d *PostgresDatastore[Message]) ReclaimWithCursor(ctx context.Context, consumerGroup string, limit int, leaseDuration time.Duration) (*ClaimedRange, error) {
	tx, err := d.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	// if finds reclaimable lease - delete it immediately
	reclaimSql := `
		DELETE FROM leases
		WHERE (token, consumer_group) IN (
			SELECT token, consumer_group FROM leases
			WHERE consumer_group = $1
				AND until < now()
			LIMIT 1
			FOR UPDATE SKIP LOCKED
		)
		RETURNING *;
	`

	leaseRows, err := tx.Query(ctx, reclaimSql, consumerGroup)
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

	return d.ClaimMessages(
		ctx, tx, consumerGroup, lease.Low, lease.High, limit, leaseDuration,
	)
}

func (d *PostgresDatastore[Message]) FreshClaimMessagesWithCursor(ctx context.Context, consumerGroup string, limit int, leaseDuration time.Duration) (*ClaimedRange, error) {
	tx, err := d.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	// TODO - projector could likely tracked head in a RWMutex such that it doesn't need to be calculated here
	// TODO - consider if we should lock any rows like claimed cursor during this tx
	cursorSql := `
		WITH old_values AS ( -- PG18+ has old / new syntax in returning but we want older version compatibility so use CTE
			SELECT * FROM cursors
			WHERE consumer_group = $1
		)
		UPDATE cursors
		SET
			claimed = LEAST(cursors.claimed + $2, (SELECT MAX(id) FROM message_log)) -- move claimed frontier forward by max(batchLimit)
		FROM old_values
		WHERE cursors.consumer_group = $1
			AND cursors.claimed < (SELECT MAX(id) FROM message_log)
		RETURNING
			old_values.claimed AS low,
			cursors.claimed AS high;
	`

	cursorRows, err := tx.Query(ctx, cursorSql, consumerGroup, limit)
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
		ctx, tx, consumerGroup, claimedRange.Low, claimedRange.High, limit, leaseDuration,
	)
}

func (d *PostgresDatastore[Message]) ClaimMessages(
	ctx context.Context,
	tx pgx.Tx,
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
		INSERT INTO leases (consumer_group, low, high, until)
		VALUES (
			$1, 
			$2, 
			$3, 
			now() + make_interval(secs => $4)
		)
		RETURNING *;
	`

	leaseRows, err := tx.Query(ctx, leaseSql, consumerGroup, low, high, leaseDuration.Seconds())
	if err != nil {
		return nil, err
	}

	lease, err := pgx.CollectOneRow(leaseRows, pgx.RowToStructByName[LeaseRow])
	if err != nil {
		return nil, err
	}

	claimSql := `
		SELECT * FROM message_log
		WHERE id > $1
			AND id <= $2 -- TODO should consider if low or high should be inclusive or not
		ORDER BY id; -- the cursor is a high-water mark: rows MUST come back in offset
		             -- order or LIMIT returns an arbitrary subset and the cursor can
		             -- advance past unread offsets (silent message loss).
	`

	messageRows, err := tx.Query(ctx, claimSql, low, high)
	if err != nil {
		return nil, err
	}

	msgs, err := pgx.CollectRows(messageRows, pgx.RowToStructByName[MessageRow])
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	return &ClaimedRange{Lease: lease, Messages: msgs}, nil
}

func (d *PostgresDatastore[Message]) ClaimMessagesWithLifecycle(ctx context.Context, consumerGroup string, limit int) ([]DeliveryRow, error) {
	// Claim this group's own delivery rows and move them 'ready' -> 'processing' in
	// one statement (the Phase 2 state machine, now per-(group, message) instead of
	// per-message). SKIP LOCKED keeps competing workers from grabbing the same row.
	//
	// deliveries only stores message_id, not the payload, so we join message_log
	// back in -- the log stays immutable, all mutation lives in deliveries.
	//
	// Phase 6 deliberately has no lease: a 'processing' row that never gets resolved
	// (consumer crash) just sits there. Visibility-timeout reclaim is Phase 6.5.
	sql := `
		WITH claimed AS (
			UPDATE deliveries
			SET
				status = 'processing',
				attempts = attempts + 1,
				updated_at = now()
			WHERE (consumer_group, message_id) IN (
				SELECT consumer_group, message_id FROM deliveries
				WHERE consumer_group = $1
					AND status = 'ready'
				ORDER BY message_id
				LIMIT $2
				FOR UPDATE SKIP LOCKED
			)
			RETURNING consumer_group, message_id, status, attempts
		)
		SELECT
			c.consumer_group,
			c.message_id,
			c.status,
			c.attempts,
			m.payload
		FROM claimed c
		JOIN message_log m ON m.id = c.message_id
		ORDER BY c.message_id;
	`

	rows, err := d.Pool.Query(ctx, sql, consumerGroup, limit)
	if err != nil {
		return nil, err
	}

	return pgx.CollectRows(rows, pgx.RowToStructByName[DeliveryRow])
}

// RecordSuccess marks a claimed delivery 'done'. Terminal success for this
// (group, message); the log row is untouched and other groups are unaffected.
func (d *PostgresDatastore[Message]) RecordSuccess(ctx context.Context, delivery *DeliveryRow) error {
	sql := `
		UPDATE deliveries
		SET
			status = 'done',
			last_error = NULL,
			updated_at = now()
		WHERE consumer_group = $1
			AND message_id = $2;
	`

	_, err := d.Pool.Exec(ctx, sql, delivery.ConsumerGroup, delivery.MessageId)
	return err
}

// RecordFailure handles a processing error: retry until attempts are exhausted,
// then hand off to RecordTerminal (the per-group DLQ). attempts was already
// incremented at claim time, so >= maxAttempts means this was the last try.
// Phase 6 has no backoff (the deliveries table carries no can_run_after) -- a
// 'ready' row is simply re-claimed on the next poll.
func (d *PostgresDatastore[Message]) RecordFailure(ctx context.Context, maxAttempts int, delivery *DeliveryRow, failureErr error) error {
	if delivery.Attempts >= maxAttempts {
		return d.RecordTerminal(ctx, delivery, failureErr)
	}

	sql := `
		UPDATE deliveries
		SET
			status = 'ready',
			last_error = $3,
			updated_at = now()
		WHERE consumer_group = $1
			AND message_id = $2;
	`

	_, err := d.Pool.Exec(ctx, sql, delivery.ConsumerGroup, delivery.MessageId, failureErr.Error())
	return err
}

// RecordTerminal dead-letters a delivery: no more retries. The DLQ for a group is
// just `WHERE consumer_group = $1 AND status = 'dead'`; one group can dead-letter a
// message while another processes the same offset fine.
func (d *PostgresDatastore[Message]) RecordTerminal(ctx context.Context, delivery *DeliveryRow, terminalErr error) error {
	sql := `
		UPDATE deliveries
		SET
			status = 'dead',
			last_error = $3,
			updated_at = now()
		WHERE consumer_group = $1
			AND message_id = $2;
	`

	_, err := d.Pool.Exec(ctx, sql, delivery.ConsumerGroup, delivery.MessageId, terminalErr.Error())
	return err
}

// func (d *PostgresDatastore[Message]) ClaimMessages(ctx context.Context, limit int, leaseDuration time.Duration) ([]MessageRow, error) {
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

// 	rows, err := d.Pool.Query(ctx, sql, limit, leaseDuration.Seconds())
// 	if err != nil {
// 		return nil, err
// 	}

// 	return pgx.CollectRows(rows, pgx.RowToStructByName[MessageRow])
// }

// func (d *PostgresDatastore[Message]) ForceReclaim(ctx context.Context, messageRow *MessageRow) error {
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

// 	_, err := d.Pool.Exec(ctx, sql, messageRow.Id, messageRow.LeaseToken)
// 	if err != nil {
// 		return err
// 	}

// 	return nil
// }

// func (d *PostgresDatastore[Message]) RecordSuccess(ctx context.Context, messageRow *MessageRow) error {
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

// 	tags, err := d.Pool.Exec(ctx, sql, messageRow.Id, messageRow.LeaseToken)
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

// func (d *PostgresDatastore[Message]) RecordFailure(ctx context.Context, maxAttempts int, messageRow *MessageRow, failureErr error) error {
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

// 		tags, err := d.Pool.Exec(ctx, sql, messageRow.Id, messageRow.LeaseToken, failureErr.Error(), backoff(messageRow.Attempts).Seconds())
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

// func (d *PostgresDatastore[Message]) RecordTerminal(ctx context.Context, messageRow *MessageRow, terminalErr error) error {
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

// 	tags, err := d.Pool.Exec(ctx, sql, messageRow.Id, messageRow.LeaseToken, terminalErr.Error())
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

func (d *PostgresDatastore[Message]) Shutdown(ctx context.Context) error {
	d.Pool.Close()

	return nil
}
