package main

// Phase 6.5c lab: watch the waterline pin on a failing message, then jump past it.
//
// Drives the real datastore methods directly (Commit, AdvanceWaterline,
// ClaimExceptions, RecordExceptionSuccess) so the pin/jump is deterministic and
// asserted on exact cursor state, not inferred from timing.
//
// Confirms: a parked exception pins committed below it even while LATER ranges
// keep claiming and committing fine (the exception window never blocks fresh
// range claims), and once the exception resolves, committed jumps straight past
// it to catch up with claimed.

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/agentstax/vulkan/pkg/consumer"
)

const group = "phase65c.lab"

// the lab drives the datastore directly (no consumerFunc), so the payload type
// is irrelevant -- a placeholder keeps it self-contained like reclaimlab's.
type payload struct{}

func main() {
	ctx := context.Background()

	ds, err := consumer.NewPostgresDatastore[payload](ctx, &consumer.PostgresConnectionParams{
		User: "example_user", Pass: "example_password",
		Host: "localhost", Port: 5432, Database: "example_db",
	})
	must(err)

	reset(ctx, ds) // clean slate for this group (shared message_log untouched)
	head := scalar(ctx, ds, `SELECT COALESCE(max(id),0) FROM message_log`)
	fmt.Printf("message_log head = %d, group = %q\n", head, group)

	const lease = 5 * time.Second
	const batch = 5
	const maxRangeReclaims = 3 // never hit in this lab -- no crashed/reclaimed ranges here

	// ===== range 1: message 3 fails, the rest succeed =====
	step("claim range 1 (ids 1-5), message 3 fails processing")
	claim1, err := ds.ClaimMessagesWithCursor(ctx, group, batch, maxRangeReclaims, lease)
	must(err)
	if claim1 == nil {
		die("expected a fresh claim, got nil (no work?)")
	}
	fmt.Printf("  claimed (%d,%d]  ids=%v\n", claim1.Lease.Low, claim1.Lease.High, ids(claim1.Messages))

	const failingId = int64(3)
	exceptions := []consumer.MessageException{{MessageId: failingId, Err: "simulated processing failure"}}
	must(ds.Commit(ctx, group, claim1.Lease.Token, exceptions, nil))
	assert("one parked exception", deliveries(ctx, ds), 1)

	committed := advance(ctx, ds)
	fmt.Printf("  roller tick -> committed = %d\n", committed)
	assert("committed pins below the failing message", committedCol(ctx, ds), failingId-1)

	// ===== range 2: fully succeeds, but committed stays pinned on message 3 =====
	step("claim + commit range 2 (ids 6-10), all succeed")
	claim2, err := ds.ClaimMessagesWithCursor(ctx, group, batch, maxRangeReclaims, lease)
	must(err)
	if claim2 == nil {
		die("expected a fresh claim, got nil")
	}
	must(ds.Commit(ctx, group, claim2.Lease.Token, nil, nil))
	committed = advance(ctx, ds)
	fmt.Printf("  claimed (%d,%d], committed after roller tick = %d\n", claim2.Lease.Low, claim2.Lease.High, committed)
	assert("claimed moved past the pin", claimedCol(ctx, ds), claim2.Lease.High)
	assert("committed still pinned on the unresolved exception", committedCol(ctx, ds), failingId-1)
	fmt.Println("  -> a parked exception never blocks fresh ranges from claiming/committing, only the waterline")

	// Commit's park always sets an initial 5s can_run_after -- the exception isn't
	// claimable until that backoff passes, same as reclaimlab's lease-expiry wait.
	step("sleep 5.5s — let the parked exception's initial backoff pass")
	time.Sleep(5500 * time.Millisecond)

	// ===== drain the exception window: message 3 retried and succeeds =====
	step("ClaimExceptions drains message 3, retry succeeds")
	claimedExceptions, err := ds.ClaimExceptions(ctx, group, batch, 3, lease)
	must(err)
	if len(claimedExceptions) != 1 || claimedExceptions[0].MessageId != failingId {
		die(fmt.Sprintf("expected to claim exactly message %d, got %+v", failingId, claimedExceptions))
	}
	fmt.Printf("  claimed exception message_id=%d attempts=%d\n", claimedExceptions[0].MessageId, claimedExceptions[0].Attempts)
	must(ds.RecordExceptionSuccess(ctx, &claimedExceptions[0]))
	assert("exception pop-deleted on success", deliveries(ctx, ds), 0)

	// ===== committed jumps straight past the resolved exception =====
	step("roller tick — committed jumps past the resolved exception")
	committed = advance(ctx, ds)
	fmt.Printf("  committed = %d\n", committed)
	assert("committed jumped to claimed", committedCol(ctx, ds), claimedCol(ctx, ds))

	// ===== drain the rest so committed reaches head =====
	step("drain remaining ranges -> committed reaches head")
	for range 10 {
		c, err := ds.ClaimMessagesWithCursor(ctx, group, batch, maxRangeReclaims, lease)
		must(err)
		if c == nil {
			break // caught up
		}
		must(ds.Commit(ctx, group, c.Lease.Token, nil, nil))
		fmt.Printf("  drained (%d,%d] -> committed = %d\n", c.Lease.Low, c.Lease.High, advance(ctx, ds))
	}
	assert("committed reached head", committedCol(ctx, ds), head)
	assert("no deliveries left behind", deliveries(ctx, ds), 0)

	fmt.Println("\n✅ PHASE 6.5c LAB PASSED")
	fmt.Println("   failure parked as an exception -> waterline pinned below it while later ranges")
	fmt.Println("   kept committing -> exception resolved -> waterline jumped straight past it.")
}

// ---- helpers ----

func reset(ctx context.Context, ds *consumer.PostgresDatastore[payload]) {
	for _, q := range []string{
		`DELETE FROM leases WHERE consumer_group=$1`,
		`DELETE FROM deliveries WHERE consumer_group=$1`,
		`DELETE FROM cursors WHERE consumer_group=$1`,
	} {
		_, err := ds.Pool.Exec(ctx, q, group)
		must(err)
	}
	must(ds.UpsertCursor(ctx, group))
}

func advance(ctx context.Context, ds *consumer.PostgresDatastore[payload]) int64 {
	c, err := ds.AdvanceWaterline(ctx, group)
	must(err)
	return c
}

func committedCol(ctx context.Context, ds *consumer.PostgresDatastore[payload]) int64 {
	return scalar(ctx, ds, `SELECT committed FROM cursors WHERE consumer_group=$1`, group)
}
func claimedCol(ctx context.Context, ds *consumer.PostgresDatastore[payload]) int64 {
	return scalar(ctx, ds, `SELECT claimed FROM cursors WHERE consumer_group=$1`, group)
}
func deliveries(ctx context.Context, ds *consumer.PostgresDatastore[payload]) int64 {
	return scalar(ctx, ds, `SELECT count(*) FROM deliveries WHERE consumer_group=$1`, group)
}

func scalar(ctx context.Context, ds *consumer.PostgresDatastore[payload], q string, args ...any) int64 {
	var v int64
	must(ds.Pool.QueryRow(ctx, q, args...).Scan(&v))
	return v
}

func ids(msgs []consumer.MessageRow) []int64 {
	out := make([]int64, len(msgs))
	for i, m := range msgs {
		out[i] = m.Id
	}
	return out
}

func step(s string)  { fmt.Printf("\n--- %s ---\n", s) }
func must(err error) {
	if err != nil {
		die(err.Error())
	}
}
func die(msg string) {
	fmt.Printf("\n❌ LAB FAILED: %s\n", msg)
	os.Exit(1)
}
func assert(label string, got, want int64) {
	if got != want {
		die(fmt.Sprintf("%s: got %d, want %d", label, got, want))
	}
	fmt.Printf("  ✓ %s (%d)\n", label, got)
}
