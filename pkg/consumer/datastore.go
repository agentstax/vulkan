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
	ClaimMessages(ctx context.Context, batchLimit int, processingTimeout time.Duration) ([]MessageRow, error)
	ForceReclaim(ctx context.Context, messageRow *MessageRow) error
	RecordSuccess(ctx context.Context, messageRow *MessageRow) error
	RecordFailure(ctx context.Context, maxAttempts int, messageRow *MessageRow, failureErr error) error
	RecordTerminal(ctx context.Context, messageRow *MessageRow, failureErr error) error
	Shutdown(ctx context.Context) error
}

type MessageRow struct {
	Id          int64           `db:"id"`
	Payload     json.RawMessage `db:"payload"`
	Status      string          `db:"status"`
	Attempts    int             `db:"attempts"`
	CanRunAfter time.Time       `db:"can_run_after"`
	LeaseUntil  *time.Time      `db:"lease_until"` // nullable
	LeaseToken  pgtype.UUID     `db:"lease_token"` // nullable
	LastError   *string         `db:"last_error"`  // nullable
	CreatedAt   time.Time       `db:"created_at"`
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

func (d *PostgresDatastore[Message]) ClaimMessages(ctx context.Context, limit int, leaseDuration time.Duration) ([]MessageRow, error) {
	// FOR UPDATE SKIP LOCKED makes the sub select query safe for update ie other consumers in group cannot select while being updated:
	sql := `
		UPDATE message_log
		SET 
			status = 'processing',
			lease_until = now() + make_interval(secs => $2),
			lease_token = gen_random_uuid(), -- 'owner' claims this uuid
			attempts = attempts + 1
		WHERE id IN (
			SELECT id FROM message_log
			WHERE (status = 'ready' AND can_run_after <= now())
				OR (status = 'processing' AND lease_until < now()) -- retreive any 'expired' work
			ORDER BY id
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		)
		RETURNING *;
	`

	rows, err := d.Pool.Query(ctx, sql, limit, leaseDuration.Seconds())
	if err != nil {
		return nil, err
	}

	return pgx.CollectRows(rows, pgx.RowToStructByName[MessageRow])
}

func (d *PostgresDatastore[Message]) ForceReclaim(ctx context.Context, messageRow *MessageRow) error {
	ctx, cancel := context.WithDeadline(context.WithoutCancel(ctx), *messageRow.LeaseUntil)
	defer cancel()

	sql := `
		UPDATE message_log
		SET 
			status = 'ready',
			lease_until = NULL,
			lease_token = NULL,
			last_error = NULL,
			attempts = attempts - 1,    -- undo the claim-time increment; this was never run
			can_run_after = now()
		WHERE id = $1
			AND lease_token = $2;
	`

	_, err := d.Pool.Exec(ctx, sql, messageRow.Id, messageRow.LeaseToken)
	if err != nil {
		return err
	}

	return nil
}

func (d *PostgresDatastore[Message]) RecordSuccess(ctx context.Context, messageRow *MessageRow) error {
	ctx, cancel := context.WithDeadline(context.WithoutCancel(ctx), *messageRow.LeaseUntil)
	defer cancel()

	sql := `
		UPDATE message_log
		SET 
			status = 'done',
			lease_until = NULL,
			lease_token = NULL,
			last_error = NULL
		WHERE id = $1
			AND lease_token = $2;
	`

	tags, err := d.Pool.Exec(ctx, sql, messageRow.Id, messageRow.LeaseToken)
	if err != nil {
		return err
	}
	// this should only happen if the lease_token no longer is valid
	// ie it was claimed by another worker or said another way the leased expired
	if tags.RowsAffected() == 0 {
		return ErrLeaseLost
	}

	return nil
}

func (d *PostgresDatastore[Message]) RecordFailure(ctx context.Context, maxAttempts int, messageRow *MessageRow, failureErr error) error {
	ctx, cancel := context.WithDeadline(context.WithoutCancel(ctx), *messageRow.LeaseUntil)
	defer cancel()

	if messageRow.Attempts >= maxAttempts {
		// terminal failure, no more retries
		return d.RecordTerminal(ctx, messageRow, failureErr)
	} else {
		// non terminal failure, should retry after backoff
		sql := `
			UPDATE message_log
			SET 
				status = 'ready',
				lease_until = NULL,
				lease_token = NULL,
				last_error = $3,
				can_run_after = now() + make_interval(secs => $4)
			WHERE id = $1
				AND lease_token = $2;
		`

		tags, err := d.Pool.Exec(ctx, sql, messageRow.Id, messageRow.LeaseToken, failureErr.Error(), backoff(messageRow.Attempts).Seconds())
		if err != nil {
			return err
		}
		// this should only happen if the lease_token no longer is valid
		// ie it was claimed by another worker or said another way the leased expired
		if tags.RowsAffected() == 0 {
			return ErrLeaseLost
		}
		return nil
	}
}

func (d *PostgresDatastore[Message]) RecordTerminal(ctx context.Context, messageRow *MessageRow, terminalErr error) error {
	ctx, cancel := context.WithDeadline(context.WithoutCancel(ctx), *messageRow.LeaseUntil)
	defer cancel()

	// terminal failure, no more retries
	sql := `
		UPDATE message_log
		SET 
			status = 'dead',
			lease_until = NULL,
			lease_token = NULL,
			last_error = $3
		WHERE id = $1
			AND lease_token = $2;
	`

	tags, err := d.Pool.Exec(ctx, sql, messageRow.Id, messageRow.LeaseToken, terminalErr.Error())
	if err != nil {
		return err
	}
	// this should only happen if the lease_token no longer is valid
	// ie it was claimed by another worker or said another way the leased expired
	if tags.RowsAffected() == 0 {
		return ErrLeaseLost
	}
	return nil
}

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
