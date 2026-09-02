package main

// One measurement cell of the consume-side fillfactor benchmark: pre-fills a
// fresh topic, then drains it through real ConsumerInstance.Consume calls and
// emits one JSON line on stdout. The happy path churns cursor_<id> (claim /
// advance UPDATEs) and lease_<id> (insert -> delete); a failure fraction adds
// exception-window churn on delivery_<id> (insert + claim / outcome UPDATEs).
//
// The cursor and delivery UPDATE paths ARE the thing under test, so this
// drives the library, never a SQL mirror of its statements. Fillfactor is
// applied per cell with ALTER TABLE ... SET (fillfactor) on the fresh, empty
// tables -- the library DDL stays untouched until a measured win adopts it.
//
// Flags: -prefill, -groups, -batch-limit, -message-concurrency,
// -failure-rate (fraction of prefilled messages that fail every attempt and
// dead-letter), -cursor-fillfactor / -delivery-fillfactor (0 = table
// default), -warmup, -window, -label. Connection via PGHOST/PGPORT/PGUSER/
// PGPASSWORD/PGDATABASE (defaults match env.sh: bench@localhost:5433).
// The caller owns synchronous_commit (ALTER DATABASE before the cell);
// the driver reports the value it actually saw.

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	vulkan "github.com/agentstax/vulkan/pkg/vulkan"
)

type benchMessage struct {
	Fail bool
}

func (benchMessage) SchemaVersion() int { return 1 }

const (
	phaseWarmup int32 = iota
	phaseMeasure
	phaseDone
)

// prefill runs the same shape as bench/compaction's produce load: it is
// setup, not the measurement, so its knobs stay fixed.
const (
	prefillProducers  = 3
	prefillGoroutines = 128
)

type tableStatistics struct {
	Inserted   int64 `json:"inserted"`
	Updated    int64 `json:"updated"`
	HotUpdated int64 `json:"hot_updated"`
	Deleted    int64 `json:"deleted"`
	DeadTuples int64 `json:"dead_tuples"`
	Bytes      int64 `json:"bytes"`
}

type cellResult struct {
	Label              string  `json:"label"`
	Prefill            int     `json:"prefill"`
	Groups             int     `json:"groups"`
	BatchLimit         int     `json:"batch_limit"`
	MessageConcurrency int     `json:"message_concurrency"`
	FailureRate        float64 `json:"failure_rate"`
	CursorFillfactor   int     `json:"cursor_fillfactor"`
	DeliveryFillfactor int     `json:"delivery_fillfactor"`
	Sync               string  `json:"sync"`
	WarmupSecs         int     `json:"warmup_secs"`
	WindowSecs         int     `json:"window_secs"`

	MsgsPerSecMed float64 `json:"msgs_per_sec_med"`
	MsgsPerSecP10 float64 `json:"msgs_per_sec_p10"`
	MsgsPerSecP90 float64 `json:"msgs_per_sec_p90"`

	Dispatched int64 `json:"dispatched"`
	Failed     int64 `json:"failed"`

	Cursor   tableStatistics `json:"cursor"`
	Delivery tableStatistics `json:"delivery"`
	Lease    tableStatistics `json:"lease"`
}

func main() {
	prefill := flag.Int("prefill", 2_000_000, "messages produced before consumption starts; must outlast the cell")
	groups := flag.Int("groups", 8, "consumer groups draining the topic, each its own cursor row")
	batchLimit := flag.Int("batch-limit", 100, "claim batch size; 1 = one cursor update per message claimed")
	messageConcurrency := flag.Int("message-concurrency", 64, "messages processed concurrently per group")
	failureRate := flag.Float64("failure-rate", 0, "fraction of prefilled messages that fail every attempt")
	cursorFillfactor := flag.Int("cursor-fillfactor", 0, "fillfactor for cursor_<id>; 0 = table default")
	deliveryFillfactor := flag.Int("delivery-fillfactor", 0, "fillfactor for delivery_<id>; 0 = table default")
	warmupSeconds := flag.Int("warmup", 10, "seconds consumed but not measured")
	windowSeconds := flag.Int("window", 15, "steady measurement seconds")
	label := flag.String("label", "", "tag copied into the JSON line")
	flag.Parse()

	ctx := context.Background()
	maxConns := *groups*6 + 32
	if maxConns > 180 {
		maxConns = 180
	}
	pool, err := iDatastore.NewPostgresPool(ctx, envOr("PGUSER", "bench"), envOr("PGPASSWORD", "bench"), envOr("PGHOST", "localhost"), envOr("PGDATABASE", "bench"), &iDatastore.PostgresConnectionConfig{
		Port:     envInt("PGPORT", 5433),
		MaxConns: maxConns,
	})
	must(err)
	defer pool.Close()

	client, err := vulkan.NewClient(ctx, pool, &vulkan.ClientConfig{AllowDestroy: true})
	must(err)
	ds := client.Datastore()
	must(client.RegisterSystem(ctx, nil))

	// fresh topic per cell -- clean tables, no cross-cell contamination
	topicName := fmt.Sprintf("fillfactorbench.%d", time.Now().UnixNano())
	registered, err := client.RegisterTopic(ctx, topicName, nil)
	must(err)
	defer func() {
		must(client.Topic(topicName).Destroy(ctx, &vulkan.DestroyOptions{Force: true}))
	}()

	// the tables are empty here, so ALTER alone is enough -- every page they
	// ever fill obeys the new fillfactor
	if *cursorFillfactor > 0 {
		_, err := ds.Pool.Exec(ctx, fmt.Sprintf(`ALTER TABLE cursor_%d SET (fillfactor = %d);`, registered.Id, *cursorFillfactor))
		must(err)
	}
	if *deliveryFillfactor > 0 {
		_, err := ds.Pool.Exec(ctx, fmt.Sprintf(`ALTER TABLE delivery_%d SET (fillfactor = %d);`, registered.Id, *deliveryFillfactor))
		must(err)
	}

	var errOnce sync.Once
	var firstErr error
	record := func(err error) {
		if err != nil {
			errOnce.Do(func() { firstErr = err })
		}
	}

	prefillTopic(ctx, client, topicName, *prefill, *failureRate, record)
	must(firstErr)

	// designated failures dead-letter after their retries; only the rest can
	// drain the backlog -- and every group drains it independently
	nonFailCount := int64(float64(*prefill)*(1-*failureRate)) * int64(*groups)

	var phase atomic.Int32
	var dispatchedTotal atomic.Int64
	var successTotal atomic.Int64
	var windowDispatched atomic.Int64
	var windowFailed atomic.Int64

	consumeFunc := func(ctx context.Context, message *benchMessage) error {
		dispatchedTotal.Add(1)
		if phase.Load() == phaseMeasure {
			windowDispatched.Add(1)
		}
		if message.Fail {
			if phase.Load() == phaseMeasure {
				windowFailed.Add(1)
			}
			return errors.New("bench-designated failure")
		}
		successTotal.Add(1)
		return nil
	}

	// a short retry curve so a designated failure's whole exception cycle
	// (claim, outcome, eventual dead) lands inside the cell
	consumerConfig := &vulkan.ConsumerConfig{
		ExceptionInitialBackoff: 500 * time.Millisecond,
		Message: &common.MessageOptions{
			Timeout: 5 * time.Second,
			Retry: &common.RetryPolicy{
				MaxRetries: 3,
				BaseDelay:  250 * time.Millisecond,
				MaxDelay:   time.Second,
				Exponent:   2,
			},
		},
	}

	consumeOptions := &vulkan.ConsumeOptions{
		BatchLimit:         *batchLimit,
		QueueSize:          *batchLimit * 2,
		MessageConcurrency: *messageConcurrency,
		ClaimPollRate:      250 * time.Millisecond,
	}

	consumeCtx, cancelConsume := context.WithCancel(ctx)
	defer cancelConsume()

	var consumeWg sync.WaitGroup
	for group := range *groups {
		instance, err := client.RegisterConsumer[benchMessage](ctx, fmt.Sprintf("bench-group-%02d", group), topicName, consumerConfig)
		must(err)

		consumeWg.Add(1)
		go func() {
			defer consumeWg.Done()
			record(instance.Consume(consumeCtx, consumeFunc, consumeOptions))
		}()
	}

	time.Sleep(time.Duration(*warmupSeconds) * time.Second)
	phase.Store(phaseMeasure)
	windowDispatched.Store(0)

	// per-second throughput samples over the steady window
	samples := make([]float64, 0, *windowSeconds)
	previous := int64(0)
	for range *windowSeconds {
		time.Sleep(time.Second)
		current := windowDispatched.Load()
		samples = append(samples, float64(current-previous))
		previous = current
	}
	phase.Store(phaseDone)
	cancelConsume()
	consumeWg.Wait()
	must(firstErr)

	if successTotal.Load() >= nonFailCount {
		must(fmt.Errorf("backlog drained during the cell: %d successes of %d consumable -- raise -prefill", successTotal.Load(), nonFailCount))
	}

	sort.Float64s(samples)

	// settle so per-backend pg_stat flushes land before the reads
	time.Sleep(2 * time.Second)

	result := cellResult{
		Label:              *label,
		Prefill:            *prefill,
		Groups:             *groups,
		BatchLimit:         *batchLimit,
		MessageConcurrency: *messageConcurrency,
		FailureRate:        *failureRate,
		CursorFillfactor:   *cursorFillfactor,
		DeliveryFillfactor: *deliveryFillfactor,
		Sync:               showSetting(ctx, ds, "synchronous_commit"),
		WarmupSecs:         *warmupSeconds,
		WindowSecs:         *windowSeconds,

		MsgsPerSecMed: percentileFloat(samples, 50),
		MsgsPerSecP10: percentileFloat(samples, 10),
		MsgsPerSecP90: percentileFloat(samples, 90),

		Dispatched: windowDispatched.Load(),
		Failed:     windowFailed.Load(),

		Cursor:   readTableStatistics(ctx, ds, fmt.Sprintf("cursor_%d", registered.Id)),
		Delivery: readTableStatistics(ctx, ds, fmt.Sprintf("delivery_%d", registered.Id)),
		Lease:    readTableStatistics(ctx, ds, fmt.Sprintf("lease_%d", registered.Id)),
	}
	encoded, err := json.Marshal(result)
	must(err)
	fmt.Println(string(encoded))
}

// prefillTopic produces the backlog the cell drains: unkeyed messages, a
// deterministic every-Nth slice of them marked to fail. Returns once every
// message is committed, so consumption starts against a quiet log.
func prefillTopic(ctx context.Context, client *vulkan.Client, topicName string, prefill int, failureRate float64, record func(error)) {
	instances := make([]*vulkan.ProducerInstance[benchMessage], prefillProducers)
	for i := range instances {
		instance, err := client.RegisterProducer[benchMessage](ctx, topicName, nil)
		must(err)
		instances[i] = instance
	}

	failThousandths := int64(failureRate * 1000)
	started := time.Now()

	var produced atomic.Int64
	var wg sync.WaitGroup
	for i := range prefillGoroutines {
		wg.Add(1)
		go func(instance *vulkan.ProducerInstance[benchMessage]) {
			defer wg.Done()
			for {
				sequence := produced.Add(1)
				if sequence > int64(prefill) {
					return
				}

				fail := sequence%1000 < failThousandths
				if _, err := instance.Produce(ctx, &benchMessage{Fail: fail}, nil); err != nil {
					record(err)
					return
				}
			}
		}(instances[i%prefillProducers])
	}
	wg.Wait()

	fmt.Fprintf(os.Stderr, "prefill: %d messages in %s\n", prefill, time.Since(started).Round(time.Second))
}

func readTableStatistics(ctx context.Context, ds *iDatastore.PostgresDatastore, relname string) tableStatistics {
	var statistics tableStatistics
	must(ds.Pool.QueryRow(ctx, `
		SELECT
			n_tup_ins,
			n_tup_upd,
			n_tup_hot_upd,
			n_tup_del,
			n_dead_tup,
			pg_relation_size(relid)
		FROM pg_stat_user_tables
		WHERE relname = $1;`, relname).Scan(
		&statistics.Inserted,
		&statistics.Updated,
		&statistics.HotUpdated,
		&statistics.Deleted,
		&statistics.DeadTuples,
		&statistics.Bytes,
	))
	return statistics
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
