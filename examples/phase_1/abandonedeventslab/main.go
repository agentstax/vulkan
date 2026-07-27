package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"time"

	"github.com/agentstax/vulkan/examples/phase_1/common"
	"github.com/agentstax/vulkan/pkg/admin"
	"github.com/agentstax/vulkan/pkg/concurrency"
	"github.com/agentstax/vulkan/pkg/consumer"
	consumermetrics "github.com/agentstax/vulkan/pkg/consumer/metrics"
	coredatastore "github.com/agentstax/vulkan/pkg/datastore"
	vulkanmetrics "github.com/agentstax/vulkan/pkg/metrics"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/google/uuid"
)

const group = "abandonedeventslab"

func main() {
	ctx := context.Background()
	run := time.Now().UnixNano()

	ds, err := coredatastore.NewPostgresDatastore(ctx, &coredatastore.PostgresConnectionConfig{
		User: "example_user", Pass: "example_password",
		Host: "localhost", Port: 5432, Database: "example_db",
	})
	must(err)
	defer ds.Close()

	mAdmin, err := admin.NewMessageAdmin(ds, &admin.MessageAdminConfig{AllowDestroy: true})
	must(err)
	must(mAdmin.RegisterSystem(ctx))

	metricsTopic, err := mAdmin.GetTopic(ctx, vulkanmetrics.TopicName, topic.SchemaVersion(1))
	must(err)
	if metricsTopic == nil {
		die("expected __system.metrics to exist after RegisterSystem")
	}

	topicName := fmt.Sprintf("%s.%d", group, run)
	tp, err := mAdmin.RegisterTopic(ctx, topicName, topic.SchemaVersion(1), &topic.Config{})
	must(err)
	defer func() {
		must(mAdmin.DestroyTopic(ctx, topicName, topic.SchemaVersion(1), admin.DestroyOptions{Force: true}))
	}()

	before := metricsRowCount(ctx, ds, metricsTopic.Id)

	step("driving a hard timeout so one message gets abandoned then self-clears")
	wp, err := producer.NewProducer[common.Work](tp.Name, topic.SchemaVersion(1), ds, &producer.ProducerConfig{DisableGracefulShutdown: true})
	must(err)
	must(wp.Register(ctx))
	seed(ctx, wp, 3)

	queue, err := concurrency.NewPressureQueue[consumer.Buffered](10)
	must(err)
	pool, err := concurrency.NewWorkerPoolLimiter(3)
	must(err)

	wc, err := consumer.NewMessageConsumer[common.Work](group, tp.Name, topic.SchemaVersion(1), queue, pool, ds, &consumer.ConsumerConfig{
		DisableGracefulShutdown: true,
		BatchLimit:              3,
		WorkTimeout:             300 * time.Millisecond,
		WorkTimeoutGrace:        50 * time.Millisecond,
	})
	must(err)
	must(wc.Register(ctx))

	var calls atomic.Int64
	consumerFunc := func(ctx context.Context, work *common.Work) error {
		if calls.Add(1) == 1 {
			time.Sleep(500 * time.Millisecond)
		}
		return nil
	}
	runProcessUntil(ctx, wc, consumerFunc, 5*time.Second, func() bool {
		return calls.Load() == 3
	})

	step("waiting for __system.metrics to see both the abandoned and cleared events")
	var rows []metricsRow
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		rows = metricsRowsSince(ctx, ds, metricsTopic.Id, before)
		if len(rows) >= 2 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if len(rows) != 2 {
		die(fmt.Sprintf("expected exactly 2 abandoned-routine events on __system.metrics, got %d: %+v", len(rows), rows))
	}

	abandoned, cleared := rows[0], rows[1]
	assertEqual("first event type", string(abandoned.Event.EventType), string(consumermetrics.EventAbandoned))
	assertEqual("second event type", string(cleared.Event.EventType), string(consumermetrics.EventCleared))
	assertEqual("abandoned event group", abandoned.Event.Group, group)
	assertEqual("abandoned event topic id", fmt.Sprint(abandoned.Event.TopicId), fmt.Sprint(tp.Id))
	assertEqual("abandoned/cleared share the same message id", fmt.Sprint(abandoned.Event.MessageId), fmt.Sprint(cleared.Event.MessageId))
	wantRoutingKey := fmt.Sprintf("abandoned_routine.%d.%s", tp.Id, group)
	assertEqual("abandoned event routing key", abandoned.RoutingKey, wantRoutingKey)
	assertEqual("cleared event routing key", cleared.RoutingKey, wantRoutingKey)
	fmt.Printf("  ✓ abandoned at %s, cleared at %s (self-clear latency %v)\n", abandoned.Event.At, cleared.Event.At, cleared.Event.At.Sub(abandoned.Event.At))

	fmt.Println("\n✅ ABANDONED EVENTS LAB PASSED")
}

type metricsRow struct {
	Id         int64
	RoutingKey string
	Event      consumermetrics.GoRoutineEvent
}

func metricsRowCount(ctx context.Context, ds *coredatastore.PostgresDatastore, topicID int64) int {
	var count int
	must(ds.Pool.QueryRow(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM message_log_%d`, topicID)).Scan(&count))
	return count
}

func metricsRowsSince(ctx context.Context, ds *coredatastore.PostgresDatastore, topicID int64, sinceCount int) []metricsRow {
	rows, err := ds.Pool.Query(ctx, fmt.Sprintf(`
		SELECT id, routing_key, payload FROM message_log_%d
		ORDER BY id
		OFFSET %d
	`, topicID, sinceCount))
	must(err)
	defer rows.Close()

	var out []metricsRow
	for rows.Next() {
		var id int64
		var routingKey *string
		var payload []byte
		must(rows.Scan(&id, &routingKey, &payload))

		var event consumermetrics.GoRoutineEvent
		must(json.Unmarshal(payload, &event))

		rk := ""
		if routingKey != nil {
			rk = *routingKey
		}
		out = append(out, metricsRow{Id: id, RoutingKey: rk, Event: event})
	}
	must(rows.Err())
	return out
}

func seed(ctx context.Context, wp *producer.Producer[common.Work], n int) {
	for range n {
		_, err := wp.ProduceFunc(ctx, func(ctx context.Context, tx producer.Tx, _ uuid.UUID) (*common.Work, error) {
			return common.NewWork(30, "admin@example.com")
		}, producer.ProduceOptions{})
		must(err)
	}
}

func runProcessUntil[Message any](ctx context.Context, wc *consumer.MessageConsumer[Message], consumerFunc consumer.ConsumerFunc[Message], timeout time.Duration, done func() bool) {
	runCtx, cancel := context.WithCancel(ctx)
	errCh := make(chan error, 1)
	go func() { errCh <- wc.Consume(runCtx, consumerFunc) }()

	start := time.Now()
	for !done() {
		if time.Since(start) > timeout {
			cancel()
			die(fmt.Sprintf("timed out waiting for the expected condition, Process returned: %v", <-errCh))
		}
		time.Sleep(20 * time.Millisecond)
	}

	cancel()
	if err := <-errCh; err != nil && !errors.Is(err, context.Canceled) {
		die(fmt.Sprintf("Process returned an unexpected error: %v", err))
	}
}

func assertEqual(label string, got, want string) {
	if got != want {
		die(fmt.Sprintf("%s: got %q, want %q", label, got, want))
	}
	fmt.Printf("  ✓ %s (%s)\n", label, got)
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
