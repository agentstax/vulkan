package main

// compaction_head correctness lab: concurrent-write convergence, plus the O(1)
// counterpart to compactionscalelab's linear growth curve.
//
// Part 1 -- concurrent same-key race. Every other lab in this phase only
// ever publishes sequentially, so the write path's `WHERE head_id <
// EXCLUDED.head_id` guard has never actually been exercised concurrently.
// It's load-bearing because BIGSERIAL allocates an id at INSERT time, not
// commit time, so concurrent publishes to the SAME key can commit out of id
// order under READ COMMITTED. N goroutines publish to the same key at once;
// compaction_head must converge to the TRUE max id afterward regardless of which
// transaction's UPSERT happened to commit last.
//
// Part 2 -- the O(1) rerun. compactionscalelab proved the old NOT EXISTS
// scan grows linearly with a topic's history (no early termination for a
// never-superseded key). Same checkpoints, same never-superseded row, but
// EXPLAIN ANALYZEs the NEW compaction_head lookup instead: touched partitions
// must stay flat at every checkpoint, because the lookup no longer scans
// message_log at all -- it's a single PK lookup on compaction_head plus the
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
	"github.com/agentstax/vulkan/pkg/admin"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/producer"
	topiccontroller "github.com/agentstax/vulkan/pkg/topic/controller"
	"github.com/google/uuid"
)

const scalePartitionSize = int64(10)

// same checkpoints as compactionscalelab -- this lab's whole point is a
// direct before/after comparison at identical history sizes.
var checkpoints = []int64{10, 50, 200, 500, 1000}

func main() {
	if err := run(); err != nil {
		fmt.Printf("\n❌ LAB FAILED: %s\n", err.Error())
		os.Exit(1)
	}
}

// labFailure is what die panics with; run recovers it into its error so
// main's deferred cleanup runs on a failed assertion.
type labFailure struct {
	message string
}

func (f labFailure) Error() string {
	return f.message
}

func run() (err error) {
	defer func() {
		switch recovered := recover().(type) {
		case nil:
		case labFailure:
			err = recovered
		default:
			panic(recovered)
		}
	}()
	ctx := context.Background()

	ds, err := iDatastore.NewPostgresDatastore(ctx, "example_user", "localhost", "example_db", &iDatastore.PostgresConnectionConfig{Pass: "example_password"})
	must(err)
	defer ds.Close()

	concurrentRaceScenario(ctx, ds)
	scaleCurveScenario(ctx, ds)

	fmt.Println("\n✅ LATEST KEYS RACE + SCALE LAB PASSED")
	return nil
}

// concurrentRaceScenario: N goroutines publish to the SAME key at once --
// compaction_head must land on the true max id, not whichever transaction
// happened to commit last in wall-clock time.
func concurrentRaceScenario(ctx context.Context, ds *iDatastore.PostgresDatastore) {
	step("concurrent same-key publishes converge to the true max id")

	const n = 50
	mAdmin, err := admin.NewMessageAdmin(ds, &admin.MessageAdminConfig{AllowDestroy: true})
	must(err)
	must(mAdmin.RegisterSystem(ctx, nil))

	topicName := fmt.Sprintf("phase8c.compactionheadracelab.race.%d", time.Now().UnixNano())
	tp, err := mAdmin.RegisterTopic(ctx, topicName, &topiccontroller.TopicConfig{PartitionSize: 1000})
	must(err)
	defer func() {
		must(mAdmin.DestroyTopic(ctx, topicName, admin.DestroyOptions{Force: true}))
	}()

	wp, err := producer.NewProducer[common.Work](ds, nil)
	must(err)
	wpInstance, err := wp.Register(ctx, tp.Name)
	must(err)

	compaction, err := producer.NewCompactionOptions(0)
	must(err)

	var wg sync.WaitGroup
	for range n {
		wg.Go(func() {
			_, err := wpInstance.ProduceFunc(ctx, func(ctx context.Context, tx producer.Tx, _ uuid.UUID) (*common.Work, error) {
				return common.NewWork(30, "admin@example.com")
			}, producer.ProduceOptions{MessageKey: "hot-key", Compaction: compaction})
			must(err)
		})
	}
	wg.Wait()

	var trueMax, compactionHeadValue int64
	must(ds.Pool.QueryRow(ctx, fmt.Sprintf(`SELECT MAX(id) FROM message_log_%d WHERE message_key='hot-key';`, tp.Id)).Scan(&trueMax))
	must(ds.Pool.QueryRow(ctx, fmt.Sprintf(`SELECT head_id FROM compaction_head_%d WHERE compaction_key='hot-key';`, tp.Id)).Scan(&compactionHeadValue))

	assertInt64(fmt.Sprintf("compaction_head converged to the true max id across %d concurrent publishes", n), compactionHeadValue, trueMax)
}

// scaleCurveScenario: identical seeding shape to compactionscalelab, but
// EXPLAINs the NEW lookup instead of the old scan at each checkpoint.
func scaleCurveScenario(ctx context.Context, ds *iDatastore.PostgresDatastore) {
	step("O(1) rerun: the same never-superseded row, re-measured against compaction_head as history grows")

	mAdmin, err := admin.NewMessageAdmin(ds, &admin.MessageAdminConfig{AllowDestroy: true})
	must(err)

	topicName := fmt.Sprintf("phase8c.compactionheadracelab.scale.%d", time.Now().UnixNano())
	tp, err := mAdmin.RegisterTopic(ctx, topicName, &topiccontroller.TopicConfig{PartitionSize: scalePartitionSize})
	must(err)
	defer func() {
		must(mAdmin.DestroyTopic(ctx, topicName, admin.DestroyOptions{Force: true}))
	}()

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

		touched, execMs := explainCompactionHeadLookup(ctx, ds, tp.Id)
		touchedAtEachCheckpoint = append(touchedAtEachCheckpoint, touched)
		fmt.Printf("  %-12d %-10d %-10d %-10.3f\n", target, totalRows, touched, execMs)
	}

	step("what the flat curve says")
	for i, touched := range touchedAtEachCheckpoint {
		assertInt64(fmt.Sprintf("touched exactly 1 message_log partition at %d partitions of history", checkpoints[i]), int64(touched), 1)
	}
	fmt.Println("  -> unlike compactionscalelab's old NOT EXISTS scan (touched grew with history size,")
	fmt.Println("     no early termination possible), this lookup never scans message_log's history at")
	fmt.Println("     all -- it's a single PK lookup on compaction_head plus the row's own id, flat by")
	fmt.Println("     construction regardless of how much history piles up behind it")
}

// ---- helpers ----

// insertStaleRow bypasses the write path (like compactionscalelab's bulk
// seeding, this cares about query cost at scale, not seeding realism) so
// its own compaction_head row is set directly alongside it.
func insertStaleRow(ctx context.Context, ds *iDatastore.PostgresDatastore, topicId int64) {
	_, err := ds.Pool.Exec(ctx, fmt.Sprintf(`INSERT INTO message_log_%d (payload, schema_version, message_key, compaction_rank) VALUES ('{}'::jsonb, 1, 'stale', 0);`, topicId))
	must(err)
	_, err = ds.Pool.Exec(ctx, fmt.Sprintf(`INSERT INTO compaction_head_%d (compaction_key, head_id, schema_version) VALUES ('stale', 1, 1);`, topicId))
	must(err)
}

// createPartitions issues every CREATE TABLE ... PARTITION OF statement for
// [from, to) as ONE multi-statement Exec -- a network round trip per
// partition would dominate the lab's own runtime at these checkpoint sizes.
func createPartitions(ctx context.Context, ds *iDatastore.PostgresDatastore, topicId, from, to int64) {
	if to <= from {
		return
	}
	logTable := fmt.Sprintf("message_log_%d", topicId)
	var sql strings.Builder
	for n := from; n < to; n++ {
		fmt.Fprintf(&sql, "CREATE TABLE IF NOT EXISTS %s_%d PARTITION OF %s FOR VALUES FROM (%d) TO (%d);\n",
			logTable, n, logTable, n*scalePartitionSize, (n+1)*scalePartitionSize)
	}
	_, err := ds.Pool.Exec(ctx, sql.String())
	must(err)
}

// bulkInsertFiller adds `count` unkeyed rows in one set-based INSERT --
// unkeyed traffic never touches compaction_head, so it's free filler for
// growing the topic's row count/tail position without affecting what's
// being measured.
func bulkInsertFiller(ctx context.Context, ds *iDatastore.PostgresDatastore, topicId, count int64) {
	if count <= 0 {
		return
	}
	sql := fmt.Sprintf(`
		INSERT INTO message_log_%d (payload, schema_version, message_key)
		SELECT '{}'::jsonb, 1, NULL FROM generate_series(1, $1);
	`, topicId)
	_, err := ds.Pool.Exec(ctx, sql, count)
	must(err)
}

// explainCompactionHeadLookup EXPLAIN ANALYZEs the production predicate --
// counting only message_log partitions the Append node ACTUALLY EXECUTED
// against (mentions alone don't mean touched, see compactionwidthlab).
func explainCompactionHeadLookup(ctx context.Context, ds *iDatastore.PostgresDatastore, topicId int64) (int, float64) {
	logTable := fmt.Sprintf("message_log_%d", topicId)
	sql := fmt.Sprintf(`
		EXPLAIN (ANALYZE, COSTS OFF) SELECT 1 FROM %s m
		WHERE m.id = 1
			AND (
				m.compaction_rank IS NULL
				OR m.id = (SELECT head_id FROM compaction_head_%d
					WHERE compaction_key = m.message_key)
			);
	`, logTable, topicId)

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
	panic(labFailure{message: msg})
}
func assertInt64(label string, got, want int64) {
	if got != want {
		die(fmt.Sprintf("%s: got %d, want %d", label, got, want))
	}
	fmt.Printf("  ✓ %s (%d)\n", label, got)
}
