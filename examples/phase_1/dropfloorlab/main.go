package main

// Phase 8a lab (b): partition-drop is a hole in the log, and the waterline
// floor decides whether a lagging group is allowed to fall into it.
//
// Same schema swap as partitionlab (lab a) and for the same reason: dropping
// a whole partition needs an actual partition boundary crossed, which
// message_log_0's migration-shipped 1,000,000-row width makes impractical at
// lab scale. Swapped in for this run, restored on exit -- see partitionlab's
// comment for why this is safe (no FK ties message_log to cursors) and what
// it costs (message_log's existing rows, permanently).
//
// Confirms three things about DropExpiredPartitions:
//  1. a group already past a partition's range is unaffected by its drop --
//     its next claim just reads on from where it was, from a partition that
//     never went anywhere.
//  2. a group whose cursor sits inside the soon-to-be-dropped range claims an
//     EMPTY batch once it's gone, yet still advances its `claimed` frontier
//     past the hole -- FreshClaimMessagesWithCursor computes the next range
//     from id arithmetic against MAX(id), never from what rows still exist.
//  3. the drop floor (MIN(committed) across cursors) refuses to drop a
//     partition a lagging group hasn't committed past yet, and
//     AllowDropPastCommitted is the explicit override that waives it.

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/agentstax/vulkan/examples/phase_1/common"
	"github.com/agentstax/vulkan/pkg/consumer"
	"github.com/agentstax/vulkan/pkg/producer"
	prodstore "github.com/agentstax/vulkan/pkg/producer/datastore"
	"github.com/jackc/pgx/v5"
)

const (
	partitionSize = int64(5)
	ttl           = 100 * time.Millisecond
	ttlMargin     = 300 * time.Millisecond // sleep past ttl with generous headroom, same convention as reclaimlab/exceptionlab's backoff waits

	groupPast   = "phase8a.drop.past"   // already committed past the range about to be dropped
	groupInside = "phase8a.drop.inside" // cursor still sits inside it
)

func main() {
	ctx := context.Background()

	cd, err := consumer.NewPostgresDatastore[common.Work](ctx, &consumer.PostgresConnectionParams{
		User: "example_user", Pass: "example_password",
		Host: "localhost", Port: 5432, Database: "example_db",
	})
	must(err)

	pd, err := prodstore.NewPostgresDatastore[common.Work](ctx, &prodstore.PostgresConnectionParams{
		User: "example_user", Pass: "example_password",
		Host: "localhost", Port: 5432, Database: "example_db",
	})
	must(err)
	wp := producer.NewWorkProducer(pd)

	step("swap message_log to a lab-scale partition width (5 rows) -- restored on exit")
	recreateMessageLog(ctx, cd, partitionSize)
	defer func() {
		recreateMessageLog(ctx, cd, 1000000)
		fmt.Println("  message_log restored to migration 001 shape")
	}()

	step("publish ids 1-4 into message_log_0, then let them age past ttl")
	for range 4 {
		publish(ctx, wp)
		must(cd.EnsureNextPartition(ctx, partitionSize, 1))
	}
	time.Sleep(ttl + ttlMargin)

	step("publish ids 5-9 into message_log_1 -- fresh, rolls the active partition forward")
	for range 5 {
		publish(ctx, wp)
		must(cd.EnsureNextPartition(ctx, partitionSize, 1))
	}
	assertPartitions("message_log_0/1/2 exist (2 is create-ahead)", partitionNumbers(ctx, cd), []int64{0, 1, 2})

	step("groupPast fast-forwards past message_log_0's range (claimed=committed=4) before the drop")
	reset(ctx, cd, groupPast)
	setCursor(ctx, cd, groupPast, 4, 4)

	step("drop -- floor sits at groupPast's committed=4, exactly message_log_0's last id, so it's not blocked")
	must(cd.DropExpiredPartitions(ctx, partitionSize, ttl, false))
	assertPartitions("message_log_0 dropped", partitionNumbers(ctx, cd), []int64{1, 2})

	step("groupPast claims on -- unaffected by the drop, reads real messages from message_log_1")
	claim := freshClaim(ctx, cd, groupPast, 5)
	assertInt("claimed advances 4 -> 9", claim.Lease.High, 9)
	assertInt("5 real messages came back", int64(len(claim.Messages)), 5)

	step("groupInside starts fresh (claimed=committed=0) -- its position now sits inside the dropped range")
	reset(ctx, cd, groupInside)

	step("groupInside claims (0,4] -- exactly the dropped range")
	claim = freshClaim(ctx, cd, groupInside, 4)
	assertInt("claimed still advances 0 -> 4 (id arithmetic, not row existence)", claim.Lease.High, 4)
	assertInt("but the batch is empty -- those rows are gone", int64(len(claim.Messages)), 0)
	fmt.Println("  -> a dropped partition is a hole a lagging cursor walks straight over, not a stall")

	step("publish ids 10-14 into message_log_2, then let message_log_1's rows (5-9) age past ttl")
	time.Sleep(ttl + ttlMargin)
	for range 5 {
		publish(ctx, wp)
		must(cd.EnsureNextPartition(ctx, partitionSize, 1))
	}
	assertPartitions("message_log_1/2/3 exist (3 is create-ahead)", partitionNumbers(ctx, cd), []int64{1, 2, 3})

	step("drop attempt -- groupInside's committed is still 0, floor blocks message_log_1 (last id 9 > floor 0)")
	must(cd.DropExpiredPartitions(ctx, partitionSize, ttl, false))
	assertPartitions("message_log_1 survives -- refused by the floor", partitionNumbers(ctx, cd), []int64{1, 2, 3})

	step("same drop, AllowDropPastCommitted=true -- the floor check is waived outright")
	must(cd.DropExpiredPartitions(ctx, partitionSize, ttl, true))
	assertPartitions("message_log_1 dropped once the floor is overridden", partitionNumbers(ctx, cd), []int64{2, 3})

	fmt.Println("\n✅ DROP FLOOR LAB PASSED")
	fmt.Println("   a group past the drop is unaffected, a lagging cursor claims empty and advances")
	fmt.Println("   over the hole, and the floor refuses a drop until it's committed past it or waived.")
}

// ---- schema swap (see partitionlab) ----

func recreateMessageLog(ctx context.Context, cd *consumer.PostgresDatastore[common.Work], firstPartitionWidth int64) {
	must(exec(ctx, cd, `DROP TABLE IF EXISTS message_log CASCADE;`))
	must(exec(ctx, cd, `
		CREATE TABLE message_log (
			id BIGSERIAL PRIMARY KEY,
			routing_key TEXT,
			payload JSONB NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		) PARTITION BY RANGE (id);
	`))
	must(exec(ctx, cd, fmt.Sprintf(`
		CREATE TABLE message_log_0
			PARTITION OF message_log
			FOR VALUES FROM (0) TO (%d);
	`, firstPartitionWidth)))
}

// ---- helpers ----

func publish(ctx context.Context, wp *producer.WorkProducer[common.Work]) {
	_, err := wp.Produce(ctx, func(ctx context.Context, tx pgx.Tx) (*common.Work, error) {
		return common.NewWork(30, "admin@example.com")
	}, "")
	must(err)
}

func reset(ctx context.Context, cd *consumer.PostgresDatastore[common.Work], group string) {
	for _, q := range []string{
		`DELETE FROM leases WHERE consumer_group=$1`,
		`DELETE FROM deliveries WHERE consumer_group=$1`,
		`DELETE FROM cursors WHERE consumer_group=$1`,
	} {
		_, err := cd.Pool.Exec(ctx, q, group)
		must(err)
	}
	must(cd.UpsertCursor(ctx, group))
}

func setCursor(ctx context.Context, cd *consumer.PostgresDatastore[common.Work], group string, claimed, committed int64) {
	_, err := cd.Pool.Exec(ctx, `UPDATE cursors SET claimed=$2, committed=$3 WHERE consumer_group=$1`, group, claimed, committed)
	must(err)
}

func freshClaim(ctx context.Context, cd *consumer.PostgresDatastore[common.Work], group string, limit int) *consumer.ClaimedRange {
	claim, err := cd.FreshClaimMessagesWithCursor(ctx, group, limit, 30*time.Second)
	must(err)
	if claim == nil {
		die(fmt.Sprintf("%s: expected a claim, got nil (already caught up?)", group))
	}
	return claim
}

func partitionNumbers(ctx context.Context, cd *consumer.PostgresDatastore[common.Work]) []int64 {
	rows, err := cd.Pool.Query(ctx, `
		SELECT REPLACE(c.relname, 'message_log_', '')::bigint AS n
		FROM pg_inherits i
		JOIN pg_class c ON c.oid = i.inhrelid
		WHERE i.inhparent = 'message_log'::regclass
			AND c.relname LIKE 'message_log_%'
		ORDER BY n;
	`)
	must(err)
	defer rows.Close()

	var out []int64
	for rows.Next() {
		var n int64
		must(rows.Scan(&n))
		out = append(out, n)
	}
	must(rows.Err())
	return out
}

func exec(ctx context.Context, cd *consumer.PostgresDatastore[common.Work], sql string) error {
	_, err := cd.Pool.Exec(ctx, sql)
	return err
}

func step(s string) { fmt.Printf("\n--- %s ---\n", s) }
func must(err error) {
	if err != nil {
		die(err.Error())
	}
}
func die(msg string) {
	fmt.Printf("\n❌ LAB FAILED: %s\n", msg)
	os.Exit(1)
}
func assertInt(label string, got, want int64) {
	if got != want {
		die(fmt.Sprintf("%s: got %d, want %d", label, got, want))
	}
	fmt.Printf("  ✓ %s (%d)\n", label, got)
}
func assertPartitions(label string, got, want []int64) {
	if len(got) != len(want) {
		die(fmt.Sprintf("%s: got %v, want %v", label, got, want))
	}
	for i := range got {
		if got[i] != want[i] {
			die(fmt.Sprintf("%s: got %v, want %v", label, got, want))
		}
	}
	fmt.Printf("  ✓ %s %v\n", label, got)
}
