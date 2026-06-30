// Package waterline is a runnable reference implementation of the hybrid
// "waterline" managed-cursor message platform described in
// bench/scale/waterline_design_v2_hybrid.md.
//
// It is the END-STATE of LEARNING_PLAN.md: an immutable append-only log
// (events) plus a sparse per-(group) exception window (deliveries), reconciled
// by a lazily-advanced waterline cursor. The happy path CLAIMS A RANGE straight
// from the log (no per-event rows) — that is the throughput win the benchmark
// measured (460-770k units/s vs ~124k for per-event rows). Only offsets that
// fall off the happy path get a deliveries row, drained pop-delete.
//
// This is reference / teaching code: it favours clarity and a single coherent
// package over the layered abstractions in pkg/. It deliberately does NOT import
// anything from pkg/ so it can be read and run on its own.
//
// The six load-bearing correctness invariants (R1-R6, from the 2026-06-21
// adversarial review) are implemented inline and re-asserted by pglog_test.go.
package waterline

import (
	"context"
	_ "embed"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed schema.sql
var schemaSQL string

// ErrLeaseLost means our lease/row was reclaimed or stolen (token mismatch); the
// operation was a no-op and the new owner is responsible for the work. Callers
// treat it as benign: stop, do not park, do not retry the same range.
var ErrLeaseLost = errors.New("waterline: lease lost (reclaimed or expired)")

// DeliveryState is the lifecycle state of a row in the sparse exception window.
type DeliveryState string

const (
	Ready    DeliveryState = "ready"    // an exception awaiting (re)processing
	Inflight DeliveryState = "inflight" // an exception leased to a worker
	Dead     DeliveryState = "dead"     // dead-lettered; retained below the line as the DLQ
	// (no Acked: a successfully (re)processed exception is DELETEd, not marked)
)

// Event is one log record the happy path reads directly. Payload is nil for a
// compaction tombstone.
type Event struct {
	Offset       int64
	Topic        *string
	RoutingKey   *string
	PartitionKey *string
	Payload      []byte
}

// Range is a reserved (Lo, Hi] slice of the log held under a lease.
type Range struct {
	Group string
	Lane  int
	Lo    int64       // exclusive low (old frontier); Lo<0 means "nothing to do"
	Hi    int64       // inclusive high (new frontier)
	Token pgtype.UUID // lease token; guards Commit against a stolen/expired lease
}

// Empty reports whether the range reserved nothing (caught up).
func (r Range) Empty() bool { return r.Lo < 0 || r.Hi <= r.Lo }

// Delivery is one (group, offset) row in the sparse exception window.
type Delivery struct {
	Group        string
	Lane         int
	Offset       int64
	PartitionKey *string
	State        DeliveryState
	Attempts     int
	LeaseToken   pgtype.UUID
}

// Exception is an offset that failed first-pass processing on the happy path.
type Exception struct {
	Offset int64
	Err    string
}

// Log is the hybrid platform API. The happy path (Claim/Reclaim/Commit) moves
// ranges with no per-event rows; the exception window (ClaimExceptions/Ack/
// Nack/DeadLetter) gives per-message lifecycle; Advance/Watermark roll the
// waterline up lazily off the hot path.
type Log interface {
	Claim(ctx context.Context, group string, lane, batch int, lease time.Duration) (Range, []Event, error)
	Reclaim(ctx context.Context, group string, lease time.Duration, maxReclaims int) (Range, []Event, bool, error)
	Commit(ctx context.Context, r Range, exceptions []Exception) error

	ClaimExceptions(ctx context.Context, group string, n, maxAttempts int, lease time.Duration) ([]Delivery, []Event, error)
	Ack(ctx context.Context, d *Delivery) error
	Nack(ctx context.Context, maxAttempts int, d *Delivery, cause error) error
	DeadLetter(ctx context.Context, d *Delivery, cause error) error

	Advance(ctx context.Context, group string, lane int) (int64, error)
	Watermark(ctx context.Context, group string) (int64, error)
}

// PgLog is the Postgres implementation of Log.
type PgLog struct {
	Pool *pgxpool.Pool
}

// New opens a pool and returns a PgLog. Caller owns Close.
func New(ctx context.Context, dsn string) (*PgLog, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &PgLog{Pool: pool}, nil
}

// Close releases the pool.
func (l *PgLog) Close() {
	if l.Pool != nil {
		l.Pool.Close()
	}
}

// Migrate (re)creates the schema. Destructive: it DROPs the tables first, so it
// is for the reference harness / tests against a throwaway DB, not production.
func (l *PgLog) Migrate(ctx context.Context) error {
	_, err := l.Pool.Exec(ctx, schemaSQL)
	return err
}

var _ Log = (*PgLog)(nil)
