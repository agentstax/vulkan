package main

// One measurement cell of the compaction hot-key serialization benchmark:
// hammers the real batched Produce path (ProducerInstance.Produce, no
// caller idempotency key -> the batcher) at one compaction-key cardinality
// and emits one JSON line on stdout.
//
// The batcher's ascending-key sort and batch sharing ARE the thing under
// test, so this drives the library, never a SQL mirror of its statements.
//
// Flags: -cardinality (0 = unkeyed baseline), -producers, -goroutines,
// -warmup, -window, -label. Connection via PGHOST/PGPORT/PGUSER/
// PGPASSWORD/PGDATABASE (defaults match env.sh: bench@localhost:5433).
// The caller owns synchronous_commit (ALTER DATABASE before the cell);
// the driver reports the value it actually saw.

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	vulkan "github.com/agentstax/vulkan/pkg/vulkan"
)

type benchMessage struct {
	Note string
}

func (benchMessage) SchemaVersion() int { return 1 }

const (
	phaseWarmup int32 = iota
	phaseMeasure
	phaseDone
)

type cellResult struct {
	Label       string `json:"label"`
	Cardinality int    `json:"cardinality"`
	Producers   int    `json:"producers"`
	Goroutines  int    `json:"goroutines"`
	Sync        string `json:"sync"`
	WarmupSecs  int    `json:"warmup_secs"`
	WindowSecs  int    `json:"window_secs"`

	MsgsPerSecMed float64 `json:"msgs_per_sec_med"`
	MsgsPerSecP10 float64 `json:"msgs_per_sec_p10"`
	MsgsPerSecP90 float64 `json:"msgs_per_sec_p90"`
	LatencyP50Ms  float64 `json:"latency_p50_ms"`
	LatencyP95Ms  float64 `json:"latency_p95_ms"`
	LatencyP99Ms  float64 `json:"latency_p99_ms"`

	Produced       int64 `json:"produced"`
	Deadlocks      int64 `json:"deadlocks"`
	HeadFillfactor int   `json:"head_fillfactor"`
	HeadUpdated    int64 `json:"head_updated"`
	HeadHotUpdated int64 `json:"head_hot_updated"`
	HeadDeadTuples int64 `json:"head_dead_tuples"`
}

func main() {
	cardinality := flag.Int("cardinality", 1, "distinct message keys (compaction enabled); 0 = unkeyed baseline")
	producers := flag.Int("producers", 3, "producer instances (each its own batcher)")
	goroutines := flag.Int("goroutines", 128, "concurrent Produce callers, split across producers")
	warmupSeconds := flag.Int("warmup", 5, "seconds produced but not measured")
	windowSeconds := flag.Int("window", 15, "steady measurement seconds")
	headFillfactor := flag.Int("head-fillfactor", 0, "fillfactor for compaction_head_<id>; 0 = table default")
	label := flag.String("label", "", "tag copied into the JSON line")
	flag.Parse()

	ctx := context.Background()
	pool, err := vulkan.NewPostgresPool(ctx, envOr("PGUSER", "bench"), envOr("PGPASSWORD", "bench"), envOr("PGHOST", "localhost"), envOr("PGDATABASE", "bench"), &vulkan.PostgresConnectionConfig{
		Port:     envInt("PGPORT", 5433),
		MaxConns: *producers*4 + 8,
	})
	must(err)
	defer pool.Close()

	client, err := vulkan.NewClient(ctx, pool, &vulkan.ClientConfig{AllowDestroy: true})
	must(err)
	ds := client.Datastore()
	must(client.RegisterSystem(ctx, nil))

	// fresh topic per cell -- clean tables, no cross-cell contamination
	topicName := fmt.Sprintf("compactionbench.%d", time.Now().UnixNano())
	registered, err := client.RegisterTopic(ctx, topicName, nil)
	must(err)
	defer func() {
		must(client.Topic(topicName).Destroy(ctx, &vulkan.DestroyOptions{Force: true}))
	}()

	// the table is empty here, so ALTER alone is enough -- every page it ever
	// fills obeys the new fillfactor (the consume-side fillfactor audit rides
	// this driver for its compaction_head cells)
	if *headFillfactor > 0 {
		_, err := ds.Pool.Exec(ctx, fmt.Sprintf(`ALTER TABLE compaction_head_%d SET (fillfactor = %d);`, registered.Id, *headFillfactor))
		must(err)
	}

	instances := make([]*vulkan.ProducerInstance[benchMessage], *producers)
	for i := range instances {
		instance, err := client.RegisterProducer[benchMessage](ctx, topicName, nil)
		must(err)
		instances[i] = instance
	}

	keys := make([]string, *cardinality)
	for i := range keys {
		keys[i] = fmt.Sprintf("key-%05d", i)
	}

	deadlocksBefore := deadlockCount(ctx, ds)

	var phase atomic.Int32
	var windowProduced atomic.Int64
	var errOnce sync.Once
	var firstErr error
	record := func(err error) { errOnce.Do(func() { firstErr = err }) }

	// per-goroutine latency slices -- merged after the run, no shared lock
	latencies := make([][]time.Duration, *goroutines)
	var wg sync.WaitGroup
	for i := range *goroutines {
		wg.Add(1)
		go func(instance *vulkan.ProducerInstance[benchMessage], offset int) {
			defer wg.Done()
			mine := make([]time.Duration, 0, 4096)
			for produced := 0; phase.Load() != phaseDone; produced++ {
				options := &vulkan.ProduceOptions{}
				if len(keys) > 0 {
					// rotate the pool from a per-goroutine offset -- maximal
					// reverse-order pressure at enqueue, the sort's job to absorb
					compaction, err := vulkan.NewCompactionOptions(0)
					if err != nil {
						record(err)
						return
					}
					options.MessageKey = keys[(offset+produced)%len(keys)]
					options.Compaction = compaction
				}

				started := time.Now()
				if _, err := instance.Produce(ctx, &benchMessage{Note: "bench"}, options); err != nil {
					record(err)
					return
				}
				if phase.Load() == phaseMeasure {
					mine = append(mine, time.Since(started))
					windowProduced.Add(1)
				}
			}
			latencies[offset] = mine
		}(instances[i%*producers], i)
	}

	time.Sleep(time.Duration(*warmupSeconds) * time.Second)
	phase.Store(phaseMeasure)
	windowProduced.Store(0)

	// per-second throughput samples over the steady window
	samples := make([]float64, 0, *windowSeconds)
	previous := int64(0)
	for range *windowSeconds {
		time.Sleep(time.Second)
		current := windowProduced.Load()
		samples = append(samples, float64(current-previous))
		previous = current
	}
	phase.Store(phaseDone)
	wg.Wait()
	must(firstErr)

	merged := make([]time.Duration, 0, 256*1024)
	for _, mine := range latencies {
		merged = append(merged, mine...)
	}
	sort.Slice(merged, func(a, b int) bool { return merged[a] < merged[b] })
	sort.Float64s(samples)

	result := cellResult{
		Label:       *label,
		Cardinality: *cardinality,
		Producers:   *producers,
		Goroutines:  *goroutines,
		Sync:        showSetting(ctx, ds, "synchronous_commit"),
		WarmupSecs:  *warmupSeconds,
		WindowSecs:  *windowSeconds,

		MsgsPerSecMed: percentileFloat(samples, 50),
		MsgsPerSecP10: percentileFloat(samples, 10),
		MsgsPerSecP90: percentileFloat(samples, 90),
		LatencyP50Ms:  milliseconds(percentileDuration(merged, 50)),
		LatencyP95Ms:  milliseconds(percentileDuration(merged, 95)),
		LatencyP99Ms:  milliseconds(percentileDuration(merged, 99)),

		Produced:       windowProduced.Load(),
		Deadlocks:      deadlockCount(ctx, ds) - deadlocksBefore,
		HeadFillfactor: *headFillfactor,
	}
	readHeadStatistics(ctx, ds, registered.Id, &result)
	encoded, err := json.Marshal(result)
	must(err)
	fmt.Println(string(encoded))

	if result.Deadlocks != 0 {
		must(fmt.Errorf("deadlocks raised during the cell: %d -- the absence claim broke under this load", result.Deadlocks))
	}
}

func deadlockCount(ctx context.Context, ds *iDatastore.PostgresDatastore) int64 {
	// settle so per-backend pg_stat flushes land before the read
	time.Sleep(2 * time.Second)
	var count int64
	must(ds.Pool.QueryRow(ctx, `SELECT deadlocks FROM pg_stat_database WHERE datname = current_database();`).Scan(&count))
	return count
}

func readHeadStatistics(ctx context.Context, ds *iDatastore.PostgresDatastore, topicId int64, result *cellResult) {
	must(ds.Pool.QueryRow(ctx, `
		SELECT
			COALESCE(MAX(n_tup_upd), 0),
			COALESCE(MAX(n_tup_hot_upd), 0),
			COALESCE(MAX(n_dead_tup), 0)
		FROM pg_stat_user_tables
		WHERE relname = $1;`,
		fmt.Sprintf("compaction_head_%d", topicId)).Scan(
		&result.HeadUpdated,
		&result.HeadHotUpdated,
		&result.HeadDeadTuples,
	))
}

func showSetting(ctx context.Context, ds *iDatastore.PostgresDatastore, name string) string {
	var value string
	must(ds.Pool.QueryRow(ctx, `SELECT current_setting($1);`, name).Scan(&value))
	return value
}

func percentileFloat(sorted []float64, p int) float64 {
	if len(sorted) == 0 {
		return 0
	}
	return sorted[(p*(len(sorted)-1))/100]
}

func percentileDuration(sorted []time.Duration, p int) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	return sorted[(p*(len(sorted)-1))/100]
}

func milliseconds(duration time.Duration) float64 {
	return float64(duration.Microseconds()) / 1000
}

func envOr(name string, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func envInt(name string, fallback int) int {
	if value := os.Getenv(name); value != "" {
		parsed, err := strconv.Atoi(value)
		must(err)
		return parsed
	}
	return fallback
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "FAIL: "+err.Error())
		os.Exit(1)
	}
}
