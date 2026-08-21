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
	iCommon "github.com/agentstax/vulkan/pkg/common"
	consumergroupcontroller "github.com/agentstax/vulkan/pkg/consumergroup/controller"
	"github.com/agentstax/vulkan/pkg/consumergroup/messageconsumer"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	iMetrics "github.com/agentstax/vulkan/pkg/metrics"
	metricsproducer "github.com/agentstax/vulkan/pkg/metrics/producer"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/topic"
	topiccontroller "github.com/agentstax/vulkan/pkg/topic/controller"
	"github.com/agentstax/vulkan/pkg/worker"
	workercontroller "github.com/agentstax/vulkan/pkg/worker/controller"
	"github.com/google/uuid"
)

const group = "abandonedeventslab"

func main() {
	ctx := context.Background()
	run := time.Now().UnixNano()

	ds, err := iDatastore.NewPostgresDatastore(ctx, "example_user", "localhost", "example_db", &iDatastore.PostgresConnectionConfig{Pass: "example_password"})
	must(err)
	defer ds.Close()

	mAdmin, err := admin.NewMessageAdmin(ds, &admin.MessageAdminConfig{AllowDestroy: true})
	must(err)
	must(mAdmin.RegisterSystem(ctx, nil))

	metricsTopic, err := mAdmin.GetTopic(ctx, iMetrics.TopicName, topic.SchemaVersion(1))
	must(err)
	if metricsTopic == nil {
		die("expected __system.metrics to exist after RegisterSystem")
	}

	topicName := fmt.Sprintf("%s.%d", group, run)
	tp, err := mAdmin.RegisterTopic(ctx, topicName, topic.SchemaVersion(1), &topiccontroller.TopicConfig{})
	must(err)
	defer func() {
		must(mAdmin.DestroyTopic(ctx, topicName, topic.SchemaVersion(1), admin.DestroyOptions{Force: true}))
	}()

	before := metricsRowCount(ctx, ds, metricsTopic.Id)

	step("driving a hard timeout so one message gets abandoned then self-clears")
	wp, err := producer.NewProducer[common.Work](ds, nil)
	must(err)
	wpInstance, err := wp.Register(ctx, tp.Name, topic.SchemaVersion(1))
	must(err)
	seed(ctx, wpInstance, 3)

	cfg := &messageconsumer.MessageConsumerConfig{
		BatchLimit:         3,
		QueueSize:          10,
		MessageConcurrency: 3,
		Message:            &iCommon.MessageOptions{Timeout: 300 * time.Millisecond},
		TimeoutGrace:       50 * time.Millisecond,
	}
	consumerDatastore, err := consumergroupcontroller.NewConsumerGroupController(ds, nil)
	must(err)
	g, err := consumerDatastore.RegisterGroup(ctx, tp.Id, group)
	must(err)
	owner, err := iCommon.NewConsumerGroupOwner(tp.SystemId, tp.Id, g.Id, g.Name)
	must(err)

	// the abandoned-event producer outlives any one claim -- the events it
	// carries are generated as the consumer shuts down
	abandonedEvents, err := metricsproducer.NewMetricsProducer(ds, &metricsproducer.ProducerConfig{SessionFlushRate: 100 * time.Millisecond})
	must(err)
	go func() {
		must(abandonedEvents.Run(ctx, group, tp.Name, topic.SchemaVersion(1), "abandonedeventslab-session"))
	}()

	var calls atomic.Int64
	consumerFunc := func(ctx context.Context, work *common.Work) error {
		if calls.Add(1) == 1 {
			time.Sleep(500 * time.Millisecond)
		}
		return nil
	}

	definition, err := messageconsumer.NewMessageConsumerProvisioner(ds, consumerFunc, abandonedEvents, cfg)
	must(err)
	must(definition.Declare(ctx, owner))

	runProcessUntil(ctx, ds, definition, owner, 5*time.Second, func() bool {
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
	assertEqual("first event type", string(abandoned.Event.EventType), string(iMetrics.EventAbandoned))
	assertEqual("second event type", string(cleared.Event.EventType), string(iMetrics.EventCleared))
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
	Event      iMetrics.GoRoutineEvent
}

func metricsRowCount(ctx context.Context, ds *iDatastore.PostgresDatastore, topicId int64) int {
	// the session counters flush to the same topic -- only the
	// abandoned-routine events are this lab's subject
	var count int
	must(ds.Pool.QueryRow(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM message_log_%d WHERE routing_key LIKE 'abandoned_routine.%%'`, topicId)).Scan(&count))
	return count
}

func metricsRowsSince(ctx context.Context, ds *iDatastore.PostgresDatastore, topicId int64, sinceCount int) []metricsRow {
	rows, err := ds.Pool.Query(ctx, fmt.Sprintf(`
		SELECT id, routing_key, payload FROM message_log_%d
		WHERE routing_key LIKE 'abandoned_routine.%%'
		ORDER BY id
		OFFSET %d
	`, topicId, sinceCount))
	must(err)
	defer rows.Close()

	var out []metricsRow
	for rows.Next() {
		var id int64
		var routingKey *string
		var payload []byte
		must(rows.Scan(&id, &routingKey, &payload))

		var event iMetrics.GoRoutineEvent
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

func seed(ctx context.Context, wpInstance *producer.ProducerInstance[common.Work], n int) {
	for range n {
		_, err := wpInstance.ProduceFunc(ctx, func(ctx context.Context, tx producer.Tx, _ uuid.UUID) (*common.Work, error) {
			return common.NewWork(30, "admin@example.com")
		}, producer.ProduceOptions{})
		must(err)
	}
}

// no manager, so nothing respawns the execution and the lab sees exactly one
// consuming life
func runProcessUntil(ctx context.Context, ds *iDatastore.PostgresDatastore, provisioner worker.Provisioner, owner *iCommon.Owner, timeout time.Duration, done func() bool) {
	workers, err := workercontroller.NewWorkerController(ds, nil)
	must(err)
	row, err := workers.GetWorker(ctx, provisioner.Definition().Name, owner)
	must(err)

	runCtx, cancel := context.WithCancel(ctx)
	execution, err := provisioner.Provision(runCtx, row)
	must(err)

	errCh := make(chan error, 1)
	go func() { errCh <- execution.Run(runCtx) }()

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
