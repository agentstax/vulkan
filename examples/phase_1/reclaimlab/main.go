package main

// Phase 6.5b lab: crash mid-range, recover.
//
// Drives the real datastore methods directly so a "crash" is deterministic: a
// worker claims a range (which opens a lease) and then simply never CommitRanges
// it -- exactly what a process that dies mid-range leaves behind. A short lease
// lets the lab show the expiry + reclaim without real-time waiting.
//
// Confirms: no exception rows are written, committed stays pinned at the crashed
// range's lo, Reclaim re-reads the EXACT range with a ROTATED token (so the dead
// worker's later commit no-ops), and committed jumps to head once the reclaim
// completes.

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/agentstax/vulkan/pkg/consumer"
)

const group = "phase65b.lab"

// the lab never processes payloads (it proves the claim/lease/commit plumbing),
// so the datastore's Message type is irrelevant -- a placeholder keeps it self-contained.
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

	const lease = 2 * time.Second
	const batch = 10

	// ===== WORKER 1: claim a range, tick the roller, then CRASH (never commit) =====
	step("WORKER 1 claims a range, then crashes mid-range (never CommitRange)")
	claim1, err := ds.ClaimMessagesWithCursor(ctx, group, batch, lease)
	must(err)
	if claim1 == nil {
		die("expected a fresh claim, got nil (no work?)")
	}
	fmt.Printf("  claimed (%d,%d]  ids=%v  lease=%s\n",
		claim1.Lease.Low, claim1.Lease.High, ids(claim1.Messages), shortTok(claim1.Lease.Token))
	committed := advance(ctx, ds) // the lazy roller ticks while the range is in-flight
	fmt.Printf("  roller tick -> committed = %d\n", committed)
	// *** CRASH: control never reaches CommitRange(claim1) ***
	oldTok := shortTok(claim1.Lease.Token)

	snapshot(ctx, ds, "AFTER CRASH")
	assert("no exception rows written", deliveries(ctx, ds), 0)
	assert("committed pinned at range lo", committedCol(ctx, ds), claim1.Lease.Low)
	assert("claimed sits at range hi", claimedCol(ctx, ds), claim1.Lease.High)
	assert("exactly one open lease", leases(ctx, ds), 1)

	// ===== lease expiry =====
	step(fmt.Sprintf("sleep %s — let the crashed lease expire", lease+500*time.Millisecond))
	time.Sleep(lease + 500*time.Millisecond)

	// ===== WORKER 2: Reclaim-before-Claim grabs the EXACT expired range =====
	step("WORKER 2 polls: Reclaim-before-Claim picks up the expired lease")
	claim2, err := ds.ClaimMessagesWithCursor(ctx, group, batch, lease)
	must(err)
	if claim2 == nil {
		die("expected a reclaim, got nil")
	}
	fmt.Printf("  reclaimed (%d,%d]  ids=%v  NEW lease=%s (was %s)\n",
		claim2.Lease.Low, claim2.Lease.High, ids(claim2.Messages), shortTok(claim2.Lease.Token), oldTok)
	assert("reclaim re-reads exact range lo", claim2.Lease.Low, claim1.Lease.Low)
	assert("reclaim re-reads exact range hi", claim2.Lease.High, claim1.Lease.High)
	assert("reclaim re-reads same message count", int64(len(claim2.Messages)), int64(len(claim1.Messages)))
	if shortTok(claim2.Lease.Token) == oldTok {
		die("token was NOT rotated — R5 violated")
	}
	fmt.Println("  token rotated -> the dead worker's stale commit will now no-op")

	// committed is still pinned at lo while the reclaimed range is in-flight again
	committed = advance(ctx, ds)
	fmt.Printf("  roller tick (mid-reclaim) -> committed = %d\n", committed)
	assert("committed still pinned during reclaim", committedCol(ctx, ds), claim1.Lease.Low)

	// the dead WORKER 1 "resurrects" and tries to commit with its STALE token: no-op
	must(ds.CommitRange(ctx, group, claim1.Lease.Token))
	assert("stale commit freed nothing (live lease survives)", leases(ctx, ds), 1)
	assert("stale commit did not move the waterline", committedCol(ctx, ds), claim1.Lease.Low)
	fmt.Println("  dead worker's stale CommitRange was a harmless no-op")

	// WORKER 2 finishes the range for real -> free lease, roller advances
	must(ds.CommitRange(ctx, group, claim2.Lease.Token))
	committed = advance(ctx, ds)
	fmt.Printf("  reclaim committed -> roller tick -> committed = %d\n", committed)

	snapshot(ctx, ds, "AFTER RECLAIM COMMITTED")
	assert("committed released past reclaimed range", committedCol(ctx, ds), claim1.Lease.High)
	assert("crashed lease is gone", leases(ctx, ds), 0)
	assert("still no exception rows", deliveries(ctx, ds), 0)

	// ===== drain the rest so committed reaches head =====
	step("drain remaining ranges -> committed reaches head")
	for range 10 {
		c, err := ds.ClaimMessagesWithCursor(ctx, group, batch, lease)
		must(err)
		if c == nil {
			break // caught up
		}
		must(ds.CommitRange(ctx, group, c.Lease.Token))
		fmt.Printf("  drained (%d,%d] -> committed = %d\n", c.Lease.Low, c.Lease.High, advance(ctx, ds))
	}
	assert("committed reached head", committedCol(ctx, ds), head)
	assert("no leases left open", leases(ctx, ds), 0)
	assert("deliveries stayed empty the whole lab", deliveries(ctx, ds), 0)

	fmt.Println("\n✅ PHASE 6.5b LAB PASSED")
	fmt.Println("   crash mid-range -> lease expired -> exact range reclaimed (token rotated) ->")
	fmt.Println("   reprocessed -> waterline pinned at lo then jumped to head -> deliveries empty.")
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

func snapshot(ctx context.Context, ds *consumer.PostgresDatastore[payload], label string) {
	fmt.Printf("  [%s] committed=%d claimed=%d open_leases=%d deliveries=%d\n",
		label, committedCol(ctx, ds), claimedCol(ctx, ds), leases(ctx, ds), deliveries(ctx, ds))
}

func committedCol(ctx context.Context, ds *consumer.PostgresDatastore[payload]) int64 {
	return scalar(ctx, ds, `SELECT committed FROM cursors WHERE consumer_group=$1`, group)
}
func claimedCol(ctx context.Context, ds *consumer.PostgresDatastore[payload]) int64 {
	return scalar(ctx, ds, `SELECT claimed FROM cursors WHERE consumer_group=$1`, group)
}
func leases(ctx context.Context, ds *consumer.PostgresDatastore[payload]) int64 {
	return scalar(ctx, ds, `SELECT count(*) FROM leases WHERE consumer_group=$1`, group)
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

func shortTok[T fmt.Stringer](t T) string {
	s := t.String()
	if len(s) >= 8 {
		return s[:8]
	}
	return s
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
