package datastore

import (
	"context"
	"errors"
	"fmt"
	"strings"

	coredatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/jackc/pgx/v5/pgconn"
)

// matches the width in migrations/001 -- TODO make config driven alongside it
const partitionSize int64 = 1_000_000

type producerDatastore[Message any] struct {
	Datastore *coredatastore.PostgresDatastore
	Topic     *topic.Topic
}

func NewPostgresDatastore[Message any](ds *coredatastore.PostgresDatastore, t *topic.Topic) *producerDatastore[Message] {
	return &producerDatastore[Message]{
		Datastore: ds,
		Topic:     t,
	}
}

// logTable is this topic's own physical message log.
func (d *producerDatastore[Message]) logTable() string {
	return fmt.Sprintf("message_log_%d", d.Topic.Id)
}

// partitionTable is logTable's nth partition -- message_log_<topic_id>_<n>.
func (d *producerDatastore[Message]) partitionTable(n int64) string {
	return fmt.Sprintf("%s_%d", d.logTable(), n)
}

// AppendMessage self-heals a missing-partition insert: the janitor's
// create-ahead is the primary defense, this retry is the backstop for a burst
// that outran it. Rerunning producerFunc is safe because its writes all go
// through the tx that just rolled back -- that's the transactional-enqueue
// contract, not a new constraint.
func (d *producerDatastore[Message]) AppendMessage(ctx context.Context, producerFunc producer.ProducerFunc[Message], routingKey string) (*Message, error) {
	message, err := d.appendMessage(ctx, producerFunc, routingKey)
	if err == nil || !isMissingPartition(err) {
		return message, err
	}

	if err := d.ensureCoveringPartition(ctx); err != nil {
		return nil, err
	}
	return d.appendMessage(ctx, producerFunc, routingKey)
}

// isMissingPartition matches an insert routed to a partition that doesn't exist yet.
func isMissingPartition(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) &&
		pgErr.Code == "23514" && // check_violation doubles as partition-routing failure
		strings.Contains(pgErr.Message, "no partition of relation")
}

// ensureCoveringPartition creates the partition after head's, so the retry's
// fresh id has somewhere to land. Headroom beyond that is the janitor's job.
func (d *producerDatastore[Message]) ensureCoveringPartition(ctx context.Context) error {
	headSql := `
		SELECT COALESCE(MAX(id), 0) FROM message_log;
	`

	var head int64
	if err := d.Datastore.Pool.QueryRow(ctx, headSql).Scan(&head); err != nil {
		return err
	}

	next := head/partitionSize + 1

	createPartitionSql := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS message_log_%d
			PARTITION OF message_log
			FOR VALUES FROM (%d) TO (%d);
	`, next, next*partitionSize, (next+1)*partitionSize)

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

func (d *producerDatastore[Message]) appendMessage(ctx context.Context, producerFunc producer.ProducerFunc[Message], routingKey string) (*Message, error) {
	tx, err := d.Datastore.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}

	// If Commit() is called successfully, Rollback() becomes a no-op and returns pgx.ErrTxClosed.
	defer tx.Rollback(ctx)

	// let user do transactional enqueue and return work/message
	message, err := producerFunc(ctx, tx)
	if err != nil {
		return nil, err
	}

	sql := `
		INSERT INTO message_log (payload, routing_key)
		VALUES (
			$1, 
			NULLIF($2, '') -- if routing_key is empty string '' insert as NULL
		);
	`

	_, err = tx.Exec(ctx, sql, message, routingKey)
	if err != nil {
		return nil, err
	}

	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}

	return message, nil
}
