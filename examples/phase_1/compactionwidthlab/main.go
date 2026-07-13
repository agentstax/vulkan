package main

// Log compaction width/planner lab: measures the read-cost tradeoff the
// unbounded compaction predicate creates (see LEARNING_PLAN.md's 8c "Open
// question" bullet -- read that first, this lab is what turns it into a
// number).
//
// Proving a row IS the latest for its key (NOT EXISTS a newer one) has no
// early termination -- it costs one partition scan per partition from that
// row's own partition through the topic's CURRENT last one. Proving it
// ISN'T (a newer row exists somewhere) can stop as soon as a match is
// found, wherever that happens to be.
//
// Registers two topics seeded with the IDENTICAL 40-message workload,
// differing only in PartitionSize (narrow vs wide, an order of magnitude
// apart), so the same two EXPLAIN checks can be compared side by side:
//   - id 1 ("stale") is never superseded -- the "prove a negative" case.
//   - id 39/40 ("fresh" v1/v2) are two versions published back to back --
//     the "find a match" case, with the match one partition away at most.

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/agentstax/vulkan/pkg/consumer"
	coredatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	narrowPartitionSize = int64(4)
	widePartitionSize   = int64(50)
)

type Record struct {
	Key string `json:"key"`
}

func main() {
	ctx := context.Background()

	ds, err := coredatastore.NewPostgresDatastore(ctx, &coredatastore.PostgresConnectionConfig{
		User: "example_user", Pass: "example_password",
		Host: "localhost", Port: 5432, Database: "example_db",
	})
	must(err)

	cd := consumer.NewConsumerDatastore[Record](ds, nil)
	pd := producer.NewProducerDatastore[Record](ds, nil)

	narrowName := fmt.Sprintf("phase8c.compactionwidthlab.narrow.%d", time.Now().UnixNano())
	narrow, err := topic.Register(ctx, ds, topic.Config{Name: narrowName, PartitionSize: narrowPartitionSize})
	must(err)
	defer func() { must(topic.Destroy(ctx, ds, narrowName)) }()

	wideName := fmt.Sprintf("phase8c.compactionwidthlab.wide.%d", time.Now().UnixNano())
	wide, err := topic.Register(ctx, ds, topic.Config{Name: wideName, PartitionSize: widePartitionSize})
	must(err)
	defer func() { must(topic.Destroy(ctx, ds, wideName)) }()

	step("seed both topics with the identical 40-message workload")
	seed(ctx, cd, producer.NewWorkProducer(narrow, pd), narrow.Id, narrowPartitionSize)
	seed(ctx, cd, producer.NewWorkProducer(wide, pd), wide.Id, widePartitionSize)

	narrowPartitions := countPartitions(ctx, ds, narrow.Id)
	widePartitions := countPartitions(ctx, ds, wide.Id)
	fmt.Printf("  narrow: PartitionSize=%d -> %d partitions\n", narrowPartitionSize, narrowPartitions)
	fmt.Printf("  wide:   PartitionSize=%d -> %d partition(s)\n", widePartitionSize, widePartitions)

	step("narrow topic: EXPLAIN the compaction check for the negative and match cases")
	negNarrow, negNarrowPlan := explainCompactionTouches(ctx, ds, narrow.Id, 1, "prove a negative (\"stale\", id=1)")
	posNarrow, posNarrowPlan := explainCompactionTouches(ctx, ds, narrow.Id, 39, "find a match (\"fresh\" v1, id=39)")
	fmt.Println("\n  --- narrow / negative case plan ---")
	fmt.Print(negNarrowPlan)
	fmt.Println("  --- narrow / match case plan ---")
	fmt.Print(posNarrowPlan)

	step("wide topic: same two checks")
	negWide, _ := explainCompactionTouches(ctx, ds, wide.Id, 1, "prove a negative (\"stale\", id=1)")
	posWide, _ := explainCompactionTouches(ctx, ds, wide.Id, 39, "find a match (\"fresh\" v1, id=39)")

	step("what the numbers say")
	assertTrue("narrow: proving a negative touches more partitions than finding a match",
		negNarrow > posNarrow)
	assertTrue("narrow: proving a negative touches nearly every partition -- no early termination",
		int64(negNarrow) >= narrowPartitions-1)
	assertTrue("wide: one partition holds everything, so both cases cost the same trivial touch",
		negWide == posWide && negWide == 1)

	fmt.Println("\n✅ COMPACTION WIDTH LAB — numbers gathered, see LEARNING_PLAN.md's 8c")
	fmt.Println("   \"Open question\" bullet for what they mean and whether they change anything.")
}

// ---- helpers ----

// seed publishes the SAME 40-message shape regardless of topic: id 1 is a
// key that's never superseded, ids 2-38 are unique filler (each its own key,
// so none of them ever match another row's compaction subplan), and ids
// 39/40 are two versions of one key published back to back.
func seed(ctx context.Context, cd consumer.Datastore[Record], wp *producer.WorkProducer[Record], topicID, partitionSize int64) {
	publish(ctx, wp, "stale") // id 1 -- never superseded
	must(cd.EnsureNextPartition(ctx, topicID, partitionSize, 1))
	for i := range 37 {
		publish(ctx, wp, fmt.Sprintf("filler:%d", i)) // ids 2-38, each a distinct key
		must(cd.EnsureNextPartition(ctx, topicID, partitionSize, 1))
	}
	publish(ctx, wp, "fresh") // id 39, v1
	must(cd.EnsureNextPartition(ctx, topicID, partitionSize, 1))
	publish(ctx, wp, "fresh") // id 40, v2 -- immediately supersedes id 39
	must(cd.EnsureNextPartition(ctx, topicID, partitionSize, 1))
}

func publish(ctx context.Context, wp *producer.WorkProducer[Record], key string) {
	_, err := wp.Produce(ctx, func(ctx context.Context, tx pgx.Tx, _ uuid.UUID) (*Record, error) {
		return &Record{Key: key}, nil
	}, producer.ProduceOptions{CompactionKey: key})
	must(err)
}

func countPartitions(ctx context.Context, ds *coredatastore.PostgresDatastore, topicID int64) int64 {
	return scalar(ctx, ds, fmt.Sprintf(`
		SELECT count(*) FROM pg_inherits i
		JOIN pg_class c ON c.oid = i.inhrelid
		WHERE i.inhparent = 'message_log_%d'::regclass;
	`, topicID))
}

// explainCompactionTouches EXPLAIN ANALYZEs just the compaction predicate
// (isolated from the bindings clause, which is a separate concern) for one
// row, and counts partitions the Append node ACTUALLY EXECUTED against.
//
// Every partition the "newer" subplan could statically apply to is always
// LISTED in the plan (Append always enumerates every child), so counting
// mentions alone can't tell scanned from skipped -- Postgres tags a child
// "(never executed)" when the anti-join's early termination (or runtime
// partition pruning) meant it was never actually opened. Only lines WITHOUT
// that tag count as a real touch.
func explainCompactionTouches(ctx context.Context, ds *coredatastore.PostgresDatastore, topicID, id int64, label string) (int, string) {
	logTable := fmt.Sprintf("message_log_%d", topicID)
	sql := fmt.Sprintf(`
		EXPLAIN (ANALYZE, COSTS OFF, TIMING OFF) SELECT 1 FROM %s m
		WHERE m.id = $1
			AND NOT EXISTS (
				SELECT 1 FROM %s newer
				WHERE newer.compaction_key = m.compaction_key
					AND newer.id > m.id
			);
	`, logTable, logTable)

	rows, err := ds.Pool.Query(ctx, sql, id)
	must(err)
	defer rows.Close()

	partitionRe := regexp.MustCompile(regexp.QuoteMeta(logTable) + `_\d+`)
	executed := map[string]bool{}
	var plan strings.Builder
	for rows.Next() {
		var line string
		must(rows.Scan(&line))
		plan.WriteString(line)
		plan.WriteString("\n")
		matches := partitionRe.FindAllString(line, -1)
		if len(matches) == 0 {
			continue
		}
		if strings.Contains(line, "never executed") {
			continue // listed in the plan, but the Append never actually opened it
		}
		for _, m := range matches {
			executed[m] = true
		}
	}
	must(rows.Err())

	names := make([]string, 0, len(executed))
	for n := range executed {
		names = append(names, n)
	}
	sort.Strings(names)
	fmt.Printf("  %s: ACTUALLY EXECUTED against %d partition(s): %v\n", label, len(names), names)
	return len(names), plan.String()
}

func scalar(ctx context.Context, ds *coredatastore.PostgresDatastore, q string, args ...any) int64 {
	var v int64
	must(ds.Pool.QueryRow(ctx, q, args...).Scan(&v))
	return v
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
func assertTrue(label string, cond bool) {
	if !cond {
		die(fmt.Sprintf("%s: got false, want true", label))
	}
	fmt.Printf("  ✓ %s\n", label)
}
