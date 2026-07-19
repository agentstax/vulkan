package producer

// THROWAWAY harness for the batcher core -- drives the unexported batcher
// directly against the dev Postgres (localhost:5432), before any public API
// exists. The permanent acceptance lab replaces this; delete alongside it.
//
// Run: go test ./pkg/producer -run TestBatcher -v

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	iTopic "github.com/agentstax/vulkan/internal/topic"
	coredatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

func harnessDatastore(t *testing.T) *coredatastore.PostgresDatastore {
	t.Helper()
	ds, err := coredatastore.NewPostgresDatastore(context.Background(), &coredatastore.PostgresConnectionConfig{
		User: "example_user", Pass: "example_password",
		Host: "localhost", Port: 5432, Database: "example_db",
	})
	if err != nil {
		t.Fatalf("dev postgres unreachable: %v", err)
	}
	return ds
}

func harnessTopic(t *testing.T, ds *coredatastore.PostgresDatastore, partitionSize int64) *topic.Topic {
	t.Helper()
	name := fmt.Sprintf("batcherharness.%s.%d", t.Name()[len("TestBatcher"):], time.Now().UnixNano())
	tp, err := topic.Register(context.Background(), ds, topic.Config{Name: name, PartitionSize: partitionSize})
	if err != nil {
		t.Fatalf("register topic: %v", err)
	}
	t.Cleanup(func() {
		if err := topic.Destroy(context.Background(), ds, name); err != nil {
			t.Errorf("destroy topic: %v", err)
		}
	})
	return tp
}

type harnessPayload struct {
	Producer int `json:"producer"`
	Seq      int `json:"seq"`
}

// ConcurrentProduce: N callers through produce() land exactly once each, and
// xmin grouping proves calls actually shared transactions.
func TestBatcherConcurrentProduce(t *testing.T) {
	ctx := context.Background()
	ds := harnessDatastore(t)
	tp := harnessTopic(t, ds, 1_000_000)

	pd := NewProducerDatastore[harnessPayload](ds, nil)
	b := newBatcher(pd, tp.Id, tp.PartitionSize, batchSettings{maxBatchSize: 100})

	const producers, msgs = 50, 20
	var wg sync.WaitGroup
	errCh := make(chan error, producers*msgs)
	for p := range producers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for s := range msgs {
				if _, err := b.produce(ctx, &harnessPayload{Producer: p, Seq: s}, ProduceOptions{}); err != nil {
					errCh <- err
				}
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("produce: %v", err)
	}

	var messages, claims int
	if err := ds.Pool.QueryRow(ctx, "SELECT count(*) FROM "+iTopic.MessageLogTable(tp.Id)).Scan(&messages); err != nil {
		t.Fatal(err)
	}
	if err := ds.Pool.QueryRow(ctx, "SELECT count(*) FROM "+iTopic.IdempotencyKeyTable(tp.Id)).Scan(&claims); err != nil {
		t.Fatal(err)
	}
	if messages != producers*msgs || claims != producers*msgs {
		t.Fatalf("expected %d messages and claims, got %d messages / %d claims", producers*msgs, messages, claims)
	}

	// rows committed by one txn share xmin -- any multi-row xmin proves grouping
	var sharedTxns, maxBatch int
	row := ds.Pool.QueryRow(ctx, fmt.Sprintf(`
		SELECT count(*), COALESCE(max(c), 0) FROM (
			SELECT count(*) AS c FROM %s GROUP BY xmin HAVING count(*) > 1
		) shared;
	`, iTopic.MessageLogTable(tp.Id)))
	if err := row.Scan(&sharedTxns, &maxBatch); err != nil {
		t.Fatal(err)
	}
	if sharedTxns == 0 {
		t.Fatal("no shared transactions observed -- batching never grouped under 50 concurrent callers")
	}
	t.Logf("shared txns: %d, largest batch: %d", sharedTxns, maxBatch)
}

// DuplicateKeys: a request whose claim already exists is a zero-row no-op
// SUCCESS inside the batch -- the other requests land untouched.
func TestBatcherDuplicateKeys(t *testing.T) {
	ctx := context.Background()
	ds := harnessDatastore(t)
	tp := harnessTopic(t, ds, 1_000_000)

	pd := NewProducerDatastore[harnessPayload](ds, nil)
	b := newBatcher(pd, tp.Id, tp.PartitionSize, batchSettings{maxBatchSize: 100})

	dupKey, _ := uuid.NewV7()
	if _, err := ds.Pool.Exec(ctx, "INSERT INTO "+iTopic.IdempotencyKeyTable(tp.Id)+" (idempotency_key) VALUES ($1)", dupKey); err != nil {
		t.Fatal(err)
	}

	ops := make([]*batchOperation[harnessPayload], 3)
	for i := range ops {
		key, _ := uuid.NewV7()
		if i == 1 {
			key = dupKey
		}
		ops[i] = newBatchOperation(key, &harnessPayload{Seq: i}, ProduceOptions{})
	}
	b.resolveBatch(ctx, newBatch(ops))

	for i, op := range ops {
		<-op.response.Done()
		if op.response.Err() != nil {
			t.Fatalf("operation %d: %v", i, op.response.Err())
		}
	}
	var messages int
	if err := ds.Pool.QueryRow(ctx, "SELECT count(*) FROM "+iTopic.MessageLogTable(tp.Id)).Scan(&messages); err != nil {
		t.Fatal(err)
	}
	if messages != 2 {
		t.Fatalf("expected 2 messages (dup swallowed), got %d", messages)
	}
}

// PoisonEviction: a server-side-rejected statement is evicted by its batch
// index with its own error; the rest rerun and land. \u0000 is valid JSON to
// Go but rejected by Postgres jsonb, so it poisons server-side every rerun.
func TestBatcherPoisonEviction(t *testing.T) {
	ctx := context.Background()
	ds := harnessDatastore(t)
	tp := harnessTopic(t, ds, 1_000_000)

	pd := NewProducerDatastore[json.RawMessage](ds, nil)
	b := newBatcher(pd, tp.Id, tp.PartitionSize, batchSettings{maxBatchSize: 100})

	ops := make([]*batchOperation[json.RawMessage], 5)
	for i := range ops {
		payload := json.RawMessage(fmt.Sprintf(`{"seq": %d}`, i))
		if i == 2 {
			payload = json.RawMessage(`{"seq": "\u0000"}`)
		}
		key, _ := uuid.NewV7()
		ops[i] = newBatchOperation(key, &payload, ProduceOptions{})
	}
	b.resolveBatch(ctx, newBatch(ops))

	for i, op := range ops {
		<-op.response.Done()
		if i == 2 {
			var pgErr *pgconn.PgError
			if !errors.As(op.response.Err(), &pgErr) {
				t.Fatalf("poison operation: expected PgError, got %v", op.response.Err())
			}
			continue
		}
		if op.response.Err() != nil {
			t.Fatalf("operation %d should have survived eviction rerun: %v", i, op.response.Err())
		}
	}
	var messages int
	if err := ds.Pool.QueryRow(ctx, "SELECT count(*) FROM "+iTopic.MessageLogTable(tp.Id)).Scan(&messages); err != nil {
		t.Fatal(err)
	}
	if messages != 4 {
		t.Fatalf("expected 4 messages after poison eviction, got %d", messages)
	}
}

// ClientEncodeIsolation: a payload pgx cannot encode fails the pipeline build
// client-side with NO statement index -- the batch must isolate to singles so
// only the bad payload's caller gets the error.
func TestBatcherClientEncodeIsolation(t *testing.T) {
	ctx := context.Background()
	ds := harnessDatastore(t)
	tp := harnessTopic(t, ds, 1_000_000)

	pd := NewProducerDatastore[json.RawMessage](ds, nil)
	b := newBatcher(pd, tp.Id, tp.PartitionSize, batchSettings{maxBatchSize: 100})

	ops := make([]*batchOperation[json.RawMessage], 4)
	for i := range ops {
		payload := json.RawMessage(fmt.Sprintf(`{"seq": %d}`, i))
		if i == 1 {
			payload = json.RawMessage(`{"broken`) // json.Marshal rejects this before anything is sent
		}
		key, _ := uuid.NewV7()
		ops[i] = newBatchOperation(key, &payload, ProduceOptions{})
	}
	b.resolveBatch(ctx, newBatch(ops))

	for i, op := range ops {
		<-op.response.Done()
		if i == 1 {
			if op.response.Err() == nil {
				t.Fatal("unencodable payload should have errored")
			}
			continue
		}
		if op.response.Err() != nil {
			t.Fatalf("operation %d should have been isolated from the bad payload: %v", i, op.response.Err())
		}
	}
	var messages int
	if err := ds.Pool.QueryRow(ctx, "SELECT count(*) FROM "+iTopic.MessageLogTable(tp.Id)).Scan(&messages); err != nil {
		t.Fatal(err)
	}
	if messages != 3 {
		t.Fatalf("expected 3 messages after encode isolation, got %d", messages)
	}
}

// MissingPartitionHeal: with no janitor and PartitionSize 10, produces past
// partition 0 must self-heal -- sequentially and under concurrency.
func TestBatcherMissingPartitionHeal(t *testing.T) {
	ctx := context.Background()
	ds := harnessDatastore(t)
	tp := harnessTopic(t, ds, 10)

	pd := NewProducerDatastore[harnessPayload](ds, nil)
	b := newBatcher(pd, tp.Id, tp.PartitionSize, batchSettings{maxBatchSize: 5}) // cap <= PartitionSize so one heal covers a batch

	for s := range 15 {
		if _, err := b.produce(ctx, &harnessPayload{Seq: s}, ProduceOptions{}); err != nil {
			t.Fatalf("sequential produce %d: %v", s, err)
		}
	}

	var wg sync.WaitGroup
	errCh := make(chan error, 40)
	for p := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for s := range 5 {
				if _, err := b.produce(ctx, &harnessPayload{Producer: p, Seq: s}, ProduceOptions{}); err != nil {
					errCh <- err
				}
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("concurrent produce: %v", err)
	}

	var messages int
	if err := ds.Pool.QueryRow(ctx, "SELECT count(*) FROM "+iTopic.MessageLogTable(tp.Id)).Scan(&messages); err != nil {
		t.Fatal(err)
	}
	if messages != 55 {
		t.Fatalf("expected 55 messages across healed partitions, got %d", messages)
	}
}

// WorkerLifecycle: the worker exits once the queue drains and a later
// produce spawns a fresh one -- the spawn/exit handshake loses no requests.
func TestBatcherWorkerLifecycle(t *testing.T) {
	ctx := context.Background()
	ds := harnessDatastore(t)
	tp := harnessTopic(t, ds, 1_000_000)

	pd := NewProducerDatastore[harnessPayload](ds, nil)
	b := newBatcher(pd, tp.Id, tp.PartitionSize, batchSettings{maxBatchSize: 100})

	if _, err := b.produce(ctx, &harnessPayload{Seq: 0}, ProduceOptions{}); err != nil {
		t.Fatalf("first produce: %v", err)
	}

	// the worker's exit trails the delivery slightly -- wait it out
	deadline := time.Now().Add(2 * time.Second)
	for {
		b.queue.mu.Lock()
		workers, queued := b.queue.workers, len(b.queue.work)
		b.queue.mu.Unlock()
		if workers == 0 && queued == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("worker never exited after the queue drained")
		}
		time.Sleep(time.Millisecond)
	}

	if _, err := b.produce(ctx, &harnessPayload{Seq: 1}, ProduceOptions{}); err != nil {
		t.Fatalf("produce after worker exit: %v", err)
	}
	var messages int
	if err := ds.Pool.QueryRow(ctx, "SELECT count(*) FROM "+iTopic.MessageLogTable(tp.Id)).Scan(&messages); err != nil {
		t.Fatal(err)
	}
	if messages != 2 {
		t.Fatalf("expected 2 messages across two worker lifetimes, got %d", messages)
	}
}

// ConcurrentWorkers: a deep backlog against a tiny batch cap must spawn
// extra workers (up to the limit) -- and exactly-once delivery must survive
// them draining the queue concurrently.
func TestBatcherConcurrentWorkers(t *testing.T) {
	ctx := context.Background()
	ds := harnessDatastore(t)
	tp := harnessTopic(t, ds, 1_000_000)

	pd := NewProducerDatastore[harnessPayload](ds, nil)
	b := newBatcher(pd, tp.Id, tp.PartitionSize, batchSettings{maxBatchSize: 5}) // tiny cap so the backlog outruns one worker

	stopSampling := make(chan struct{})
	maxWorkers := make(chan int, 1)
	go func() {
		peak := 0
		for {
			select {
			case <-stopSampling:
				maxWorkers <- peak
				return
			default:
			}
			b.queue.mu.Lock()
			peak = max(peak, b.queue.workers)
			b.queue.mu.Unlock()
			time.Sleep(100 * time.Microsecond)
		}
	}()

	const producers, msgs = 30, 20
	var wg sync.WaitGroup
	errCh := make(chan error, producers*msgs)
	for p := range producers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for s := range msgs {
				if _, err := b.produce(ctx, &harnessPayload{Producer: p, Seq: s}, ProduceOptions{}); err != nil {
					errCh <- err
				}
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("produce: %v", err)
	}
	close(stopSampling)

	var messages, claims int
	if err := ds.Pool.QueryRow(ctx, "SELECT count(*) FROM "+iTopic.MessageLogTable(tp.Id)).Scan(&messages); err != nil {
		t.Fatal(err)
	}
	if err := ds.Pool.QueryRow(ctx, "SELECT count(*) FROM "+iTopic.IdempotencyKeyTable(tp.Id)).Scan(&claims); err != nil {
		t.Fatal(err)
	}
	if messages != producers*msgs || claims != producers*msgs {
		t.Fatalf("expected %d messages and claims, got %d messages / %d claims", producers*msgs, messages, claims)
	}

	if peak := <-maxWorkers; peak < 2 {
		t.Fatalf("expected backlog pressure to spawn extra workers, peak was %d", peak)
	} else {
		t.Logf("peak concurrent workers: %d", peak)
	}
}

// CompactionKeys: hot compaction keys ride inside concurrent batches -- the
// per-batch sort keeps latest_key lock order global, so nothing deadlocks
// (which would surface as an evicted operation's error) and every latest_key
// row ends pointing at the highest id written for its key.
func TestBatcherCompactionKeys(t *testing.T) {
	ctx := context.Background()
	ds := harnessDatastore(t)
	tp := harnessTopic(t, ds, 1_000_000)

	pd := NewProducerDatastore[harnessPayload](ds, nil)
	b := newBatcher(pd, tp.Id, tp.PartitionSize, batchSettings{maxBatchSize: 5}) // tiny cap -> concurrent workers -> real lock contention

	const producers, msgs, keys = 20, 20, 3
	var wg sync.WaitGroup
	errCh := make(chan error, producers*msgs)
	for p := range producers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for s := range msgs {
				key := fmt.Sprintf("hot:%d", (p+s)%keys)
				if _, err := b.produce(ctx, &harnessPayload{Producer: p, Seq: s}, ProduceOptions{CompactionKey: key}); err != nil {
					errCh <- fmt.Errorf("producer %d seq %d: %w", p, s, err)
				}
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("produce: %v", err)
	}

	var messages int
	if err := ds.Pool.QueryRow(ctx, "SELECT count(*) FROM "+iTopic.MessageLogTable(tp.Id)).Scan(&messages); err != nil {
		t.Fatal(err)
	}
	if messages != producers*msgs {
		t.Fatalf("expected %d messages, got %d", producers*msgs, messages)
	}

	// every latest_key row must point at the max id for its key
	var stale int
	row := ds.Pool.QueryRow(ctx, fmt.Sprintf(`
		SELECT count(*) FROM latest_key lk
		JOIN (
			SELECT compaction_key, max(id) AS max_id FROM %s GROUP BY compaction_key
		) m ON m.compaction_key = lk.compaction_key
		WHERE lk.topic_id = $1 AND lk.latest_id <> m.max_id;
	`, iTopic.MessageLogTable(tp.Id)), tp.Id)
	if err := row.Scan(&stale); err != nil {
		t.Fatal(err)
	}
	if stale != 0 {
		t.Fatalf("%d latest_key rows not pointing at their key's max id", stale)
	}
}

// PublicProduce: the MessageProducer routing -- plain calls batch, a
// caller-keyed call takes the per-call path and still dedups on its key.
func TestBatcherPublicProduce(t *testing.T) {
	ctx := context.Background()
	ds := harnessDatastore(t)
	tp := harnessTopic(t, ds, 1_000_000)

	pd := NewProducerDatastore[harnessPayload](ds, nil)
	wp := NewMessageProducer(tp, pd)

	const producers, msgs = 10, 10
	var wg sync.WaitGroup
	errCh := make(chan error, producers*msgs)
	for p := range producers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for s := range msgs {
				if _, err := wp.Produce(ctx, &harnessPayload{Producer: p, Seq: s}, ProduceOptions{}); err != nil {
					errCh <- err
				}
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("produce: %v", err)
	}

	// caller-keyed: same key twice must land exactly one message
	key, _ := uuid.NewV7()
	for range 2 {
		if _, err := wp.Produce(ctx, &harnessPayload{Producer: 99}, ProduceOptions{IdempotencyKey: key}); err != nil {
			t.Fatalf("caller-keyed produce: %v", err)
		}
	}

	var messages int
	if err := ds.Pool.QueryRow(ctx, "SELECT count(*) FROM "+iTopic.MessageLogTable(tp.Id)).Scan(&messages); err != nil {
		t.Fatal(err)
	}
	if messages != producers*msgs+1 {
		t.Fatalf("expected %d messages (batched + 1 deduped caller-keyed), got %d", producers*msgs+1, messages)
	}
}
