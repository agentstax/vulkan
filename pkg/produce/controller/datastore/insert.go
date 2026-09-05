package datastore

import (
	"context"
	"errors"
	"fmt"

	"github.com/agentstax/vulkan/pkg/common"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/produce"
	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/jackc/pgx/v5"
)

// produceInTxSavepoint is a fixed name, not per-call unique -- safe because
// calls only ever run sequentially against one tx (pgx.Tx isn't safe for
// concurrent use), so each use is released before the next begins.
const produceInTxSavepoint = "sp_produce_in_tx"

// runInsert runs produceFunc + the claim-protected message insert against an
// already-open tx.
func (d *ProduceDatastore) runInsert[Message common.Versioned](ctx context.Context, tx iDatastore.Tx, topicId int64, produceFunc produce.ProducerFunc[Message], data *Append[Message]) (*Appended[Message], error) {
	payload, err := produceFunc(ctx, tx)
	if err != nil {
		return nil, err
	}
	if err := checkSchemaVersion(payload); err != nil {
		return nil, err
	}

	id, duplicate, err := d.insertProtected(ctx, tx, topicId, payload, data)
	if err != nil {
		return nil, err
	}
	return &Appended[Message]{Message: payload, Id: id, Duplicate: duplicate}, nil
}

// runInsertSavepoint wraps produceFunc + the message insert in a SAVEPOINT
// scoped to just this call, so a missing-partition retry can't touch
// anything else already done in tx.
func (d *ProduceDatastore) runInsertSavepoint[Message common.Versioned](ctx context.Context, tx iDatastore.Tx, topicId int64, produceFunc produce.ProducerFunc[Message], data *Append[Message]) (*Appended[Message], error) {
	if err := commitToSavepoint(ctx, tx, produceInTxSavepoint); err != nil {
		return nil, err
	}

	payload, err := produceFunc(ctx, tx)
	if err != nil {
		attemptRollbackToSavepoint(ctx, tx, produceInTxSavepoint)
		return nil, err
	}
	if err := checkSchemaVersion(payload); err != nil {
		attemptRollbackToSavepoint(ctx, tx, produceInTxSavepoint)
		return nil, err
	}

	id, duplicate, err := d.insertProtectedSavepoint(ctx, tx, topicId, payload, data)
	if err != nil {
		attemptRollbackToSavepoint(ctx, tx, produceInTxSavepoint)
		return nil, err
	}
	return &Appended[Message]{Message: payload, Id: id, Duplicate: duplicate}, nil
}

// insertProtectedSavepoint pipelines the claim+insert CTE with RELEASE
// SAVEPOINT as one round trip -- always a single statement regardless of
// compaction, so it always fully batches. duplicate=true means the claim
// already existed.
func (d *ProduceDatastore) insertProtectedSavepoint[Message common.Versioned](ctx context.Context, q iDatastore.Querier, topicId int64, payload *Message, data *Append[Message]) (id int64, duplicate bool, err error) {
	sql, args := protectedInsertSQL(topicId, payload, data, d.Datastore.Schema)

	batch := &pgx.Batch{}
	batch.Queue(sql, args...)
	batch.Queue("RELEASE SAVEPOINT " + produceInTxSavepoint + ";")

	br := q.SendBatch(ctx, batch)
	err = br.QueryRow().Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		// claim already existed -- inserted CTE never ran. Not a failure:
		// RELEASE SAVEPOINT is still queued next and still needs reading.
		d.Logger.DebugContext(ctx, "duplicate publish detected, idempotency claim already existed", "topic_id", topicId, "idempotency_key", data.IdempotencyKey)
		duplicate = true
	} else if err != nil {
		br.Close()
		return 0, false, err
	}

	if _, err := br.Exec(); err != nil { // RELEASE SAVEPOINT
		br.Close()
		return 0, false, err
	}
	return id, duplicate, br.Close()
}

// insertProtected runs the idempotency claim + message insert (+ compaction_head
// upsert when compacted) in one round trip. duplicate=true means the claim already
// existed -- WHERE EXISTS matched nothing, Scan comes back pgx.ErrNoRows.
func (d *ProduceDatastore) insertProtected[Message common.Versioned](ctx context.Context, q iDatastore.Querier, topicId int64, payload *Message, data *Append[Message]) (id int64, duplicate bool, err error) {
	sql, args := protectedInsertSQL(topicId, payload, data, d.Datastore.Schema)

	err = q.QueryRow(ctx, sql, args...).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		d.Logger.DebugContext(ctx, "duplicate publish detected, idempotency claim already existed", "topic_id", topicId, "idempotency_key", data.IdempotencyKey)
		return 0, true, nil // claim already existed -- already committed
	}
	if err != nil {
		return 0, false, err
	}
	return id, false, nil
}

// ***************
// *** HELPERS ***
// ***************

func checkSchemaVersion[Message common.Versioned](payload *Message) error {
	if (*payload).SchemaVersion() < 1 {
		return fmt.Errorf("Message.SchemaVersion must be >= 1, got %d", (*payload).SchemaVersion())
	}
	return nil
}

func commitToSavepoint(ctx context.Context, q iDatastore.Querier, savepointName string) error {
	_, err := q.Exec(ctx, "SAVEPOINT "+savepointName+";")
	return err
}

func attemptRollbackToSavepoint(ctx context.Context, q iDatastore.Querier, savepointName string) {
	_, _ = q.Exec(ctx, "ROLLBACK TO SAVEPOINT "+savepointName+";")
}

// protectedInsertSQL builds the claim+insert(+compaction_head upsert when
// compacted) CTE -- shared with the savepoint-batched path so both run the
// exact same statement. Claims against idempotency_key_<topicId>
func protectedInsertSQL[Message common.Versioned](topicId int64, payload *Message, data *Append[Message], schema string) (string, []any) {
	// the row's schema_version is the payload's own SchemaVersion(): a constant
	// per type for user messages, the stored version for a replayed one
	args := []any{data.IdempotencyKey, payload, data.RoutingKey, int64((*payload).SchemaVersion())}

	var sql string
	if data.Compacted {
		// claim + insert + compaction_head upsert in one round trip -- inserted
		// stays empty when the claim already existed, so latest never fires either.
		sql = fmt.Sprintf(`
			-- vulkan: produce.protectedInsert
			WITH claim AS (
				INSERT INTO %[1]s.%[2]s (idempotency_key)
				VALUES ($1)
				ON CONFLICT (idempotency_key) DO NOTHING
				RETURNING idempotency_key
			), inserted AS (
				INSERT INTO %[1]s.%[3]s (payload, routing_key, schema_version, message_key, compaction_rank, options)
				SELECT $2, NULLIF($3, ''), $4, $5, $6, $7  -- if routing_key $3 is empty string '' insert as NULL
				WHERE EXISTS (SELECT 1 FROM claim) -- if claim CTE didn't return anything skip this
				RETURNING id
			), latest AS (
				INSERT INTO %[1]s.%[4]s AS h (compaction_key, head_id, schema_version, compaction_rank)
				SELECT $5, id, $4, $6 FROM inserted
				ON CONFLICT (compaction_key) DO UPDATE
				SET head_id = EXCLUDED.head_id, schema_version = EXCLUDED.schema_version, compaction_rank = EXCLUDED.compaction_rank
				-- a newer payload version always wins; within a version rank first, then head_id
				WHERE (h.schema_version, h.compaction_rank, h.head_id) < (EXCLUDED.schema_version, EXCLUDED.compaction_rank, EXCLUDED.head_id)
			)
			SELECT id FROM inserted;
		`, schema, topic.IdempotencyKeyTable(topicId), topic.MessageLogTable(topicId), topic.CompactionHeadTable(topicId))

		args = append(args, data.MessageKey, data.CompactionRank, data.Options) // $5, $6, $7
	} else {
		// claim + insert in one round trip -- WHERE EXISTS only fires if the
		// claim CTE landed a row, so a conflict makes both match zero rows.
		// compaction_rank stays NULL: this message never opted into compaction.
		sql = fmt.Sprintf(`
			-- vulkan: produce.protectedInsert
			WITH claim AS (
				INSERT INTO %[1]s.%[2]s (idempotency_key)
				VALUES ($1)
				ON CONFLICT (idempotency_key) DO NOTHING
				RETURNING idempotency_key
			)
			INSERT INTO %[1]s.%[3]s (payload, routing_key, schema_version, message_key, options)
			SELECT
				$2,
				NULLIF($3, ''), -- if routing_key is empty string '' insert as NULL
				$4,
				NULLIF($5, ''), -- if message_key is empty string '' insert as NULL
				$6
			WHERE EXISTS (SELECT 1 FROM claim) -- if claim CTE didn't return anything skip this
			RETURNING id;
		`, schema, topic.IdempotencyKeyTable(topicId), topic.MessageLogTable(topicId))

		args = append(args, data.MessageKey, data.Options) // $5, $6
	}

	return sql, args
}
