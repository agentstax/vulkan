package datastore

import (
	"context"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"
)

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

func (d *PostgresDatastore[Message]) AppendMessage(ctx context.Context, message *Message) error {
	sql := `
		INSERT INTO message_log (payload)
		VALUES ($1)
		RETURNING id
	`

	var id int
	err := d.Pool.QueryRow(ctx, sql, message).Scan(&id)
	if err != nil {
		return err
	}

	return nil
}
