package main

// latest_key correctness lab: concurrent-write convergence, plus the O(1)
// counterpart to compactionscalelab's linear growth curve.
//
// Part 1 -- concurrent same-key race. Every other lab in this phase only
// ever publishes sequentially, so the write path's `WHERE latest_id <
// EXCLUDED.latest_id` guard has never actually been exercised concurrently.
// It's load-bearing because BIGSERIAL allocates an id at INSERT time, not
// commit time, so concurrent publishes to the SAME key can commit out of id
// order under READ COMMITTED. N goroutines publish to the same key at once;
// latest_key must converge to the TRUE max id afterward regardless of which
// transaction's UPSERT happened to commit last.
//
// Part 2 -- the O(1) rerun. compactionscalelab proved the old NOT EXISTS
// scan grows linearly with a topic's history (no early termination for a
// never-superseded key). Same checkpoints, same never-superseded row, but
// EXPLAIN ANALYZEs the NEW latest_key lookup instead: touched partitions
// must stay flat at every checkpoint, because the lookup no longer scans
// message_log at all -- it's a single PK lookup on latest_key plus the
// row's own id.

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/agentstax/vulkan/examples/phase_1/common"
	coredatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const scalePartitionSize = int64(10)

// same checkpoints as compactionscalelab -- this lab's whole point is a
// direct before/after comparison at identical history sizes.
var checkpoints = []int64{10, 50, 200, 500, 1000}

func main() {
	ctx := context.Background()

	ds, err := coredatastore.NewPostgresDatastore(ctx, &coredatastore.PostgresConnectionConfig{
		User: "example_user", Pass: "example_password",
		Host: "localhost", Port: 5432, Database: "example_db",
	})
	must(err)

	concurrentRaceScenario(ctx, ds)
	scaleCurveScenario(ctx, ds)

	fmt.Println("\n✅ LATEST KEYS RACE + SCALE LAB PASSED")
}

// concurrentRaceScenario: N goroutines publish to the SAME key at once --
// latest_key must land on the true max id, not whichever transaction
// happened to commit last in wall-clock time.
func concurrentRaceScenario(ctx context.Context, ds *coredatastore.PostgresDatastore) {
	step("concurrent same-key publishes converge to the true max id")

	const n = 50
	topicName := fmt.Sprintf("phase8c.latestkeysracelab.race.%d", time.Now().UnixNano())
	tp, err := topic.Register(ctx, ds, topic.Config{Name: topicName, PartitionSize: 1000})
	must(err)
	defer func() { must(topic.Destroy(ctx, ds, topicName)) }()

	pd := producer.NewProducerDatastore[common.Work](ds, nil)
	wp := producer.NewWorkProducer(tp, pd)

	var wg sync.WaitGroup
	for range n {
		wg.Go(func() {
			_, err := wp.Produce(ctx, func(ctx context.Context, tx pgx.Tx, _ uuid.UUID) (*common.Work, error) {
				return common.NewWork(30, "admin@example.com")
			}, producer.ProduceOptions{CompactionKey: "hot-key"})
			must(err)
		})
	}
	wg.Wait()

	var trueMax, latestKeysValue int64
	must(ds.Pool.QueryRow(ctx, fmt.Sprintf(`SELECT MAX(id) FROM message_log_%d WHERE compaction_key='hot-key';`, tp.Id)).Scan(&trueMax))
	must(ds.Pool.QueryRow(ctx, `SELECT latest_id FROM latest_key WHERE topic_id=$1 AND compaction_key='hot-key';`, tp.Id).Scan(&latestKeysValue))

	assertInt64(fmt.Sprintf("latest_key converged to the true max id across %d concurrent publishes", n), latestKeysValue, trueMax)
}

// scaleCurveScenario: identical seeding shape to compactionscalelab, but
// EXPLAINs the NEW lookup instead of the old scan at each checkpoint.
func scaleCurveScenario(ctx context.Context, ds *coredatastore.PostgresDatastore) {
	step("O(1) rerun: the same never-superseded row, re-measured against latest_key as history grows")

	topicName := fmt.Sprintf("phase8c.latestkeysracelab.scale.%d", time.Now().UnixNano())
	tp, err := topic.Register(ctx, ds, topic.Config{Name: topicName, PartitionSize: scalePartitionSize})
	must(err)
	defer func() { must(topic.Destroy(ctx, ds, topicName)) }()

	insertStaleRow(ctx, ds, tp.Id)

	fmt.Printf("  %-12s %-10s %-10s %-10s\n", "partitions", "rows", "touched", "exec_ms")

	var createdPartitions int64 = 1 // partition 0 already exists from Register
	var totalRows int64 = 1
	touchedAtEachCheckpoint := make([]int, 0, len(checkpoints))

	for _, target := range checkpoints {
		createPartitions(ctx, ds, tp.Id, createdPartitions, target)
		createdPartitions = target

		targetRows := target*scalePartitionSize - 1
		bulkInsertFiller(ctx, ds, tp.Id, targetRows-totalRows)
		totalRows = targetRows

		touched, execMs := explainLatestKeyLookup(ctx, ds, tp.Id)
		touchedAtEachCheckpoint = append(touchedAtEachCheckpoint, touched)
		fmt.Printf("  %-12d %-10d %-10d %-10.3f\n", target, totalRows, touched, execMs)
	}

	step("what the flat curve says")
	for i, touched := range touchedAtEachCheckpoint {
		assertInt64(fmt.Sprintf("touched exactly 1 message_log partition at %d partitions of history", checkpoints[i]), int64(touched), 1)
	}
	fmt.Println("  -> unlike compactionscalelab's old NOT EXISTS scan (touched grew with history size,")
	fmt.Println("     no early termination possible), this lookup never scans message_log's history at")
	fmt.Println("     all -- it's a single PK lookup on latest_key plus the row's own id, flat by")
	fmt.Println("     construction regardless of how much history piles up behind it")
}

// ---- helpers ----

// insertStaleRow bypasses the write path (like compactionscalelab's bulk
// seeding, this cares about query cost at scale, not seeding realism) so
// its own latest_key row is set directly alongside it.
func insertStaleRow(ctx context.Context, ds *coredatastore.PostgresDatastore, topicID int64) {
	_, err := ds.Pool.Exec(ctx, fmt.Sprintf(`INSERT INTO message_log_%d (payload, compaction_key) VALUES ('{}'::jsonb, 'stale');`, topicID))
	must(err)
	_, err = ds.Pool.Exec(ctx, `INSERT INTO latest_key (topic_id, compaction_key, latest_id) VALUES ($1, 'stale', 1);`, topicID)
	must(err)
}

// createPartitions issues every CREATE TABLE ... PARTITION OF statement for
// [from, to) as ONE multi-statement Exec -- a network round trip per
// partition would dominate the lab's own runtime at these checkpoint sizes.
func createPartitions(ctx context.Context, ds *coredatastore.PostgresDatastore, topicID, from, to int64) {
	if to <= from {
		return
	}
	logTable := fmt.Sprintf("message_log_%d", topicID)
	var sql strings.Builder
	for n := from; n < to; n++ {
		fmt.Fprintf(&sql, "CREATE TABLE IF NOT EXISTS %s_%d PARTITION OF %s FOR VALUES FROM (%d) TO (%d);\n",
			logTable, n, logTable, n*scalePartitionSize, (n+1)*scalePartitionSize)
	}
	_, err := ds.Pool.Exec(ctx, sql.String())
	must(err)
}

// bulkInsertFiller adds `count` unkeyed rows in one set-based INSERT --
// unkeyed traffic never touches latest_key, so it's free filler for
// growing the topic's row count/tail position without affecting what's
// being measured.
func bulkInsertFiller(ctx context.Context, ds *coredatastore.PostgresDatastore, topicID, count int64) {
	if count <= 0 {
		return
	}
	sql := fmt.Sprintf(`
		INSERT INTO message_log_%d (payload, compaction_key)
		SELECT '{}'::jsonb, NULL FROM generate_series(1, $1);
	`, topicID)
	_, err := ds.Pool.Exec(ctx, sql, count)
	must(err)
}

// explainLatestKeyLookup EXPLAIN ANALYZEs the production predicate --
// counting only message_log partitions the Append node ACTUALLY EXECUTED
// against (mentions alone don't mean touched, see compactionwidthlab).
func explainLatestKeyLookup(ctx context.Context, ds *coredatastore.PostgresDatastore, topicID int64) (int, float64) {
	logTable := fmt.Sprintf("message_log_%d", topicID)
	sql := fmt.Sprintf(`
		EXPLAIN (ANALYZE, COSTS OFF) SELECT 1 FROM %s m
		WHERE m.id = 1
			AND (
				m.compaction_key IS NULL
				OR m.id = (SELECT latest_id FROM latest_key
					WHERE topic_id = %d AND compaction_key = m.compaction_key)
			);
	`, logTable, topicID)

	rows, err := ds.Pool.Query(ctx, sql)
	must(err)
	defer rows.Close()

	partitionRe := regexp.MustCompile(regexp.QuoteMeta(logTable) + `_\d+`)
	execRe := regexp.MustCompile(`Execution Time: ([\d.]+) ms`)
	executed := map[string]bool{}
	var execMs float64
	for rows.Next() {
		var line string
		must(rows.Scan(&line))

		if m := execRe.FindStringSubmatch(line); m != nil {
			execMs, _ = strconv.ParseFloat(m[1], 64)
		}
		matches := partitionRe.FindAllString(line, -1)
		if len(matches) == 0 || strings.Contains(line, "never executed") {
			continue
		}
		for _, p := range matches {
			executed[p] = true
		}
	}
	must(rows.Err())
	return len(executed), execMs
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
func assertInt64(label string, got, want int64) {
	if got != want {
		die(fmt.Sprintf("%s: got %d, want %d", label, got, want))
	}
	fmt.Printf("  ✓ %s (%d)\n", label, got)
}
