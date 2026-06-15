package datastore

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/agentstax/vulkan/pkg/consumer"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type MessageRow struct {
	Id          int64           `db:"id"`
	Payload     json.RawMessage `db:"payload"`
	Status      string          `db:"status"`
	Attempts    int             `db:"attempts"`
	CanRunAfter time.Time       `db:"can_run_after"`
	LockedAt    *time.Time      `db:"locked_at"`  // nullable
	LastError   *string         `db:"last_error"` // nullable
	CreatedAt   time.Time       `db:"created_at"`
}

type PostgresConnectionParams struct {
	User     string
	Pass     string
	Host     string
	Port     int
	Database string
}

type PostgresDatastore[Message any] struct {
	Pool *pgxpool.Pool
}

func NewPostgresDatastore[Message any](ctx context.Context, params *PostgresConnectionParams) (*PostgresDatastore[Message], error) {
	// TODO - params validate using go playground (maybe no lib)

	connectionString := fmt.Sprintf("postgres://%s:%s@%s:%s/%s",
		params.User, params.Pass, params.Host, strconv.Itoa(params.Port), params.Database,
	)

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

func (d *PostgresDatastore[Message]) ProcessMessages(
	ctx context.Context,
	batchLimit int,
	maxAttempts int,
	workTimeout time.Duration,
	consumerFunc consumer.ConsumerFunc[Message],
) error {
	messageRows, err := d.ClaimMessages(ctx, batchLimit, workTimeout)
	if err != nil {
		return err
	}
	if len(messageRows) == 0 {
		return nil
	}

	for _, messageRow := range messageRows {
		// early exit if timeout expired, later commits would fail anyway but this is cleaner
		// TODO - might consider if should record as failure here with cancel or deadline as error text
		if err := ctx.Err(); err != nil {
			return err
		}

		var message Message
		if err := json.Unmarshal(messageRow.Payload, &message); err != nil {
			// TODO - a bad payload is likely to never be retried successfully, ideally this should be immediately failed to not waste processing -> sent to dead letter queue
			if recordError := d.RecordFailure(ctx, maxAttempts, messageRow, err); recordError != nil {
				return recordError // TODO - should collect error into []error and continue to allow rest of batch to attempt to finish first
			}

			// just continue on, just b/c single message is bad doesn't mean others are
			continue
		}

		// ### PROCESS MESSAGE ###
		if err := consumerFunc(ctx, &message); err != nil {
			// TODO - consider adding a debug field which logs this error if true

			// actual processing error -> should retry till exhaust
			if recordError := d.RecordFailure(ctx, maxAttempts, messageRow, err); recordError != nil {
				return recordError // TODO - should collect error into []error and continue to allow rest of batch to attempt to finish first
			}

			// just continue on, just b/c single message is bad doesn't mean others are
			continue
		}

		// if reach here processing successful for message
		// TODO - consider adding a debug field which logs this success if true
		if recordError := d.RecordSuccess(ctx, messageRow); recordError != nil {
			return recordError // TODO - should collect error into []error and continue to allow rest of batch to attempt to finish first
		}
	}

	return nil
}

func (d *PostgresDatastore[Message]) ClaimMessages(ctx context.Context, limit int, workTimeout time.Duration) ([]MessageRow, error) {
	// FOR UPDATE SKIP LOCKED makes the sub select query safe for update ie other consumers in group cannot select while being updated:
	sql := `
		UPDATE message_log
		SET status = 'processing', locked_at = now(), attempts = attempts + 1
		WHERE id IN (
			SELECT id FROM message_log
			WHERE (status = 'ready' AND can_run_after <= now())
				OR (status = 'processing' AND locked_at < now() - make_interval(secs => $2)) -- retreive any 'stuck' work
			ORDER BY id
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		)
		RETURNING *;
	`

	// stuck timeout should always have extra buffer to not overlap
	stuckTimeout := workTimeout + (5 * time.Second)
	rows, err := d.Pool.Query(ctx, sql, limit, stuckTimeout.Seconds())
	if err != nil {
		return nil, err
	}

	return pgx.CollectRows(rows, pgx.RowToStructByName[MessageRow])
}

func (d *PostgresDatastore[Message]) RecordSuccess(ctx context.Context, messageRow MessageRow) error {
	sql := `
		UPDATE message_log
		SET status = 'done', locked_at = NULL, last_error = NULL
		WHERE id = $1;
	`

	_, err := d.Pool.Exec(ctx, sql, messageRow.Id)
	if err != nil {
		return err
	}

	return nil
}

func (d *PostgresDatastore[Message]) RecordFailure(ctx context.Context, maxAttempts int, messageRow MessageRow, failureErr error) error {
	if messageRow.Attempts >= maxAttempts {
		// terminal failure, no more retries
		sql := `
			UPDATE message_log
			SET status = 'dead', locked_at = NULL, last_error = $2
			WHERE id = $1;
		`

		_, err := d.Pool.Exec(ctx, sql, messageRow.Id, failureErr.Error())
		if err != nil {
			return err
		}
		return nil
	} else {
		// non terminal failure, should retry after backoff
		sql := `
			UPDATE message_log
			SET status = 'ready', locked_at = NULL, last_error = $2,
				can_run_after = now() + make_interval(secs => $3)
			WHERE id = $1;
		`

		_, err := d.Pool.Exec(ctx, sql, messageRow.Id, failureErr.Error(), backoff(messageRow.Attempts).Seconds())
		if err != nil {
			return err
		}
		return nil
	}
}

// TODO - backoff should be overrideable func from consumer definition
func backoff(attempts int) time.Duration {
	d := time.Second * (1 << (attempts - 1)) // 1s, 2s, 4s, ...
	if d > 5*time.Minute {
		d = 5 * time.Minute
	}
	return d
}

func (d *PostgresDatastore[Message]) Shutdown(ctx context.Context) error {
	d.Pool.Close()

	return nil
}
