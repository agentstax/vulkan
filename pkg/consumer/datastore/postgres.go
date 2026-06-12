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
	Id        int64           `db:"id"`
	Payload   json.RawMessage `db:"payload"`
	CreatedAt time.Time       `db:"created_at"`
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

func (d *PostgresDatastore[Message]) ProcessMessages(ctx context.Context, limit int, consumerFunc consumer.ConsumerFunc[Message]) error {
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return err
	}

	// If Commit() is called successfully, Rollback() becomes a no-op and returns pgx.ErrTxClosed.
	defer tx.Rollback(ctx)

	claimSql := `
		SELECT * FROM message_log
		ORDER BY id
		LIMIT $1
		FOR UPDATE SKIP LOCKED;
	`

	rows, err := tx.Query(ctx, claimSql, limit)
	if err != nil {
		return err
	}

	messageRows, err := pgx.CollectRows(rows, pgx.RowToStructByName[MessageRow])
	if err != nil {
		return err
	}
	if len(messageRows) == 0 {
		return nil
	}

	for _, messageRow := range messageRows {
		// early exit if timeout expired, later commits would fail anyway but this is cleaner
		if err := ctx.Err(); err != nil {
			return err
		}

		var message Message
		if err := json.Unmarshal(messageRow.Payload, &message); err != nil {
			return err
		}

		// ### PROCESS MESSAGE ###
		if err := consumerFunc(ctx, message); err != nil {
			return err
		}
	}

	// delete rows after processing
	var ids []int64
	for _, messageRow := range messageRows {
		ids = append(ids, messageRow.Id)
	}

	deleteSql := `
		DELETE FROM message_log
		WHERE id = ANY($1);
	`

	_, err = tx.Exec(ctx, deleteSql, ids)
	if err != nil {
		return err
	}

	// finally always commit transaction
	if err = tx.Commit(ctx); err != nil {
		return err
	}

	return nil
}

func (d *PostgresDatastore[Message]) Shutdown(ctx context.Context) error {
	d.Pool.Close()

	return nil
}
