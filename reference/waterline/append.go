package waterline

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// AppendSpec is one event to append to the log. Nil pointers / empty Payload are
// stored as SQL NULL (a NULL Payload is a compaction tombstone). Headers, if
// non-empty, must be a JSON object string.
type AppendSpec struct {
	Topic        *string
	RoutingKey   *string
	PartitionKey *string
	Headers      string // JSON object; "" -> default '{}'
	Payload      []byte // nil -> tombstone
}

// Append writes one event and returns its assigned offset.
func (l *PgLog) Append(ctx context.Context, s AppendSpec) (int64, error) {
	var off int64
	err := l.Pool.QueryRow(ctx, appendSQL, s.args()...).Scan(&off)
	return off, err
}

// AppendTx writes one event inside the CALLER's transaction (the Phase 1.5
// transactional-enqueue API: the event and a business write commit together or
// neither does).
func (l *PgLog) AppendTx(ctx context.Context, tx pgx.Tx, s AppendSpec) (int64, error) {
	var off int64
	err := tx.QueryRow(ctx, appendSQL, s.args()...).Scan(&off)
	return off, err
}

const appendSQL = `
	INSERT INTO events(topic, routing_key, partition_key, headers, payload)
	VALUES ($1, $2, $3, COALESCE($4::jsonb, '{}'::jsonb), $5::jsonb)
	RETURNING "offset";`

func (s AppendSpec) args() []any {
	var headers any
	if s.Headers != "" {
		headers = s.Headers
	}
	var payload any
	if s.Payload != nil {
		payload = string(s.Payload)
	}
	return []any{s.Topic, s.RoutingKey, s.PartitionKey, headers, payload}
}

// AppendBatch bulk-appends n events with a fixed payload via COPY (used by the
// benchmark to seed the log fast). keyer/router, if non-nil, set partition_key /
// routing_key per index. Returns the new head offset.
func (l *PgLog) AppendBatch(ctx context.Context, n int, payload []byte, router, keyer func(i int) *string) (int64, error) {
	rows := make([][]any, n)
	for i := range n {
		var rk, pk *string
		if router != nil {
			rk = router(i)
		}
		if keyer != nil {
			pk = keyer(i)
		}
		rows[i] = []any{nil, rk, pk, []byte("{}"), payload}
	}
	_, err := l.Pool.CopyFrom(ctx,
		pgx.Identifier{"events"},
		[]string{"topic", "routing_key", "partition_key", "headers", "payload"},
		pgx.CopyFromRows(rows))
	if err != nil {
		return 0, err
	}
	return l.Head(ctx)
}
