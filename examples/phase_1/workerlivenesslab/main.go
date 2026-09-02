// Command workerlivenesslab proves the worker_liveness alert end to end:
// what a Register-time pass logs, and what the scheduled check publishes and
// resolves.
//
// Sections:
//  1. register-time -- a produce-only process logs VK0063 naming the topic's
//     unclaimed topic_janitor; with a consumer running, every row on the
//     topic is claimed and the next Register is silent
//  2. scheduled -- with the group's consumer stopped, a run of the
//     worker_liveness job publishes an active alert naming the group's
//     message_consumer; restarting the consumer resolves it on the next run
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/agentstax/vulkan/pkg/alert"
	"github.com/agentstax/vulkan/pkg/alert/compactionreadcost"
	"github.com/agentstax/vulkan/pkg/alert/partitioncount"
	"github.com/agentstax/vulkan/pkg/alert/workerliveness"
	workerlivenesscontroller "github.com/agentstax/vulkan/pkg/alert/workerliveness/controller"
	"github.com/agentstax/vulkan/pkg/common"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/schedule"
	"github.com/agentstax/vulkan/pkg/topic"
	vulkan "github.com/agentstax/vulkan/pkg/vulkan"
	"github.com/agentstax/vulkan/pkg/worker"
	workercontroller "github.com/agentstax/vulkan/pkg/worker/controller"
)

const eventCode = "VK0063"

// labMessage is the lab topic's payload -- the group never has to process
// one, the lab only needs the group's worker rows to exist.
type labMessage struct {
	Value string
}

func (labMessage) SchemaVersion() int { return 1 }

var (
	ds     *iDatastore.PostgresDatastore
	client *vulkan.Client

	// registerClient logs through capture so the Register-time pass can be counted
	registerClient *vulkan.Client
	capture        *captureLogger

	schedulesTopic *topic.TopicData
	alertsTopic    *topic.TopicData
	prefix         string

	labTopic      *topic.TopicData
	labTopicOwner *common.Owner
	labGroupName  string

	jobGroup      int64
	jobGroupOwner *common.Owner
)

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

	pool, err := vulkan.NewPostgresPool(ctx, "example_user", "example_password", "localhost", "example_db", nil)
	must(err)
	defer pool.Close()

	client, err = vulkan.NewClient(ctx, pool, &vulkan.ClientConfig{AllowDestroy: true})
	must(err)
	ds = client.Datastore()
	must(client.RegisterSystem(ctx, nil))

	capture = newCaptureLogger()
	registerClient, err = vulkan.NewClient(ctx, pool, &vulkan.ClientConfig{Logger: capture})
	must(err)

	schedulesTopic, err = client.Topic(schedule.TopicName).Get(ctx)
	must(err)
	alertsTopic, err = client.Topic(alert.TopicName).Get(ctx)
	must(err)

	jobGroup = scalarInt64(ctx,
		fmt.Sprintf(`SELECT id FROM %s.consumer_group_config WHERE topic_id = $1 AND name = $2;`, ds.Schema),
		schedulesTopic.Id, workerliveness.JobName)
	jobGroupOwner, err = common.NewConsumerGroupOwner(schedulesTopic.SystemId, schedulesTopic.Id, jobGroup, workerliveness.JobName)
	must(err)

	prefix = fmt.Sprintf("workerlivenesslab.%d", time.Now().UnixNano())
	labGroupName = prefix + ".group"
	labTopic, err = client.RegisterTopic(ctx, prefix+".topic", nil)
	must(err)
	labTopicOwner, err = common.NewTopicOwner(labTopic.SystemId, labTopic.Id, labTopic.Name)
	must(err)
	defer cleanup()

	// only the lab's run-nows produce job requests (a suspended job still
	// runs on run-now)
	for _, jobName := range []string{partitioncount.JobName, compactionreadcost.JobName, workerliveness.JobName} {
		must(client.Schedule(jobName).Suspend(ctx))
	}

	registerSection(ctx)
	scheduledSection(ctx)

	fmt.Println("\n✅ WORKER LIVENESS LAB PASSED")
	fmt.Println("   a produce-only process learns nothing is running its topic's rows;")
	fmt.Println("   the scheduled check turns the same fact into an alert that resolves itself")
	return nil
}

func registerSection(ctx context.Context) {
	step("register-time: produce-only warns VK0063, a running consumer silences it")

	// a fresh topic's only worker row is its janitor, and nothing has claimed it
	_, err := registerClient.RegisterProducer[labMessage](ctx, labTopic.Name, nil)
	must(err)
	lines := capture.find(eventCode, workerlivenesscontroller.AlertWorkerLiveness, labTopic.Name)
	if len(lines) != 1 {
		die(fmt.Sprintf("produce-only Register: want 1 %s line, got %d", eventCode, len(lines)))
	}
	if detail := lines[0]["detail"]; !strings.Contains(fmt.Sprint(detail), "topic_janitor") {
		die(fmt.Sprintf("produce-only Register: the line must name the unclaimed janitor, got %v", detail))
	}
	fmt.Println("  ✓ a produce-only Register warned once, naming the unclaimed topic_janitor")

	// the consumer's manager claims the topic's rows, its own included
	stopConsumer := startConsumer(ctx)
	waitUnclaimed(ctx, 0)

	before := len(capture.find(eventCode, workerlivenesscontroller.AlertWorkerLiveness, labTopic.Name))
	_, err = registerClient.RegisterProducer[labMessage](ctx, labTopic.Name, nil)
	must(err)
	if got := len(capture.find(eventCode, workerlivenesscontroller.AlertWorkerLiveness, labTopic.Name)); got != before {
		die(fmt.Sprintf("Register under a live consumer must be silent, got %d lines after %d", got, before))
	}
	fmt.Println("  ✓ with every row claimed, the next Register said nothing")

	stopConsumer()
	waitUnclaimed(ctx, 1)
	fmt.Println("  ✓ stopping the consumer released its rows")
}

func scheduledSection(ctx context.Context) {
	step("scheduled: the check publishes an active alert, a running consumer resolves it")

	stopExecutor := startExecutor(ctx)
	defer stopExecutor()

	activeRun, err := client.Schedule(workerliveness.JobName).Run(ctx, nil)
	must(err)
	waitDelivered(ctx, activeRun.Id, "success")

	key := alertKey(labTopicOwner)
	if got := headStatus(ctx, key); got != string(alert.StatusActive) {
		die(fmt.Sprintf("the check must publish an active alert for the topic, got %q", got))
	}

	found := listedAlert(ctx)
	if found == nil {
		die("ListAlerts must carry the topic's active worker_liveness alert")
	}
	if !namesWorker(found, "message_consumer", labGroupName) {
		die(fmt.Sprintf("the alert must name the group's unclaimed message_consumer, got %v", found.Data["workers"]))
	}
	fmt.Println("  ✓ the check published an active alert naming the group's message_consumer")

	// every row claimed again -> the same check resolves what it published
	stopConsumer := startConsumer(ctx)
	defer stopConsumer()
	waitUnclaimed(ctx, 0)

	resolveRun, err := client.Schedule(workerliveness.JobName).Run(ctx, nil)
	must(err)
	waitDelivered(ctx, resolveRun.Id, "success")
	if got := headStatus(ctx, key); got != string(alert.StatusResolved) {
		die(fmt.Sprintf("a claimed fleet must resolve the alert, got %q", got))
	}
	fmt.Println("  ✓ with the consumer back, the next run resolved it")
}

// --- harness ---

// startConsumer runs a consumer on the lab topic until the returned stop is
// called; its manager claims every worker row the topic owns.
func startConsumer(ctx context.Context) func() {
	instance, err := client.RegisterConsumer[labMessage](ctx, labGroupName, labTopic.Name, nil)
	must(err)

	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() {
		done <- instance.Consume(runCtx, func(ctx context.Context, message *labMessage) error { return nil }, nil)
	}()
	return func() {
		cancel()
		must(<-done)
	}
}

// startExecutor claims the worker_liveness worker row and runs its execution
// until the returned stop is called.
func startExecutor(ctx context.Context) func() {
	provisioner, err := workerliveness.NewWorkerLivenessProvisioner(ds, nil)
	must(err)
	workers, err := workercontroller.NewWorkerController(ds, nil)
	must(err)
	row, err := workers.GetWorker(ctx, workerliveness.JobName, jobGroupOwner)
	must(err)
	if row == nil {
		die("RegisterSystem must declare the " + workerliveness.JobName + " worker row")
	}

	// a crashed earlier run's claim lingers until its InstanceTTL expires --
	// retry past it instead of dying
	var execution worker.Execution
	deadline := time.Now().Add(60 * time.Second)
	for {
		execution, err = provisioner.Provision(ctx, row)
		must(err)
		if execution != nil {
			break
		}
		if time.Now().After(deadline) {
			die("the alert worker declined the instance for 60s -- is a daemon already running?")
		}
		time.Sleep(time.Second)
	}

	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- execution.Run(runCtx) }()
	return func() {
		cancel()
		must(<-done)
	}
}

func cleanup() {
	ctx := context.Background()

	for _, jobName := range []string{partitioncount.JobName, compactionreadcost.JobName, workerliveness.JobName} {
		must(client.Schedule(jobName).Unsuspend(ctx))
	}

	// the check evaluates every topic, so a run leaves a head on each one --
	// all of them are this lab's, and nothing is left running to resolve them
	pattern := workerlivenesscontroller.AlertWorkerLiveness + "/%"
	exec(ctx, fmt.Sprintf(`DELETE FROM %s.%s WHERE compaction_key LIKE $1;`, ds.Schema, topic.CompactionHeadTable(alertsTopic.Id)), pattern)
	exec(ctx, fmt.Sprintf(`DELETE FROM %s.%s WHERE message_key LIKE $1;`, ds.Schema, topic.MessageLogTable(alertsTopic.Id)), pattern)

	must(client.Topic(labTopic.Name).Destroy(ctx, &vulkan.DestroyOptions{Force: true}))
}

// --- assertion helpers ---

// waitUnclaimed returns once the number of the lab topic's worker rows with
// no live instance is at least want -- 0 waits for every row claimed.
func waitUnclaimed(ctx context.Context, want int64) {
	sql := fmt.Sprintf(`
		SELECT COUNT(*)
		FROM %s.worker_config w
		LEFT JOIN %s.consumer_group_config g ON g.id = w.consumer_group_id
		WHERE COALESCE(w.topic_id, g.topic_id) = %d
			AND NOT EXISTS (SELECT 1 FROM %s.worker_instance i WHERE i.worker_id = w.id AND i.expires_at > now());
	`, ds.Schema, ds.Schema, labTopic.Id, ds.Schema)

	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		unclaimed := scalarInt64(ctx, sql)
		if (want == 0 && unclaimed == 0) || (want > 0 && unclaimed >= want) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	die(fmt.Sprintf("timed out waiting for %d unclaimed worker rows on the lab topic", want))
}

// listedAlert is the lab topic's worker_liveness alert as ListAlerts reads
// it, nil when the topic has none.
func listedAlert(ctx context.Context) *alert.Alert {
	heads, err := client.ListAlerts(ctx)
	must(err)
	for _, head := range heads {
		found := head.Message
		if found.Name == workerlivenesscontroller.AlertWorkerLiveness && found.Owner.Name == labTopic.Name {
			return found
		}
	}
	return nil
}

// namesWorker reports whether the alert's evidence carries the worker row
// under the owner that declared it.
func namesWorker(found *alert.Alert, workerName string, ownerName string) bool {
	rows, ok := found.Data["workers"].([]any)
	if !ok {
		return false
	}
	for _, row := range rows {
		fields, ok := row.(map[string]any)
		if ok && fields["worker"] == workerName && fields["owner"] == ownerName {
			return true
		}
	}
	return false
}

func alertKey(owner *common.Owner) string {
	key, err := alert.MessageKey(workerlivenesscontroller.AlertWorkerLiveness, owner)
	must(err)
	return key
}

// headStatus is "" when the key has no head or its payload carries no status.
func headStatus(ctx context.Context, messageKey string) string {
	sql := fmt.Sprintf(`
		SELECT m.payload->>'status'
		FROM %s.%s h
		JOIN %s.%s m ON m.id = h.head_id
		WHERE h.compaction_key = $1;
	`, ds.Schema, topic.CompactionHeadTable(alertsTopic.Id), ds.Schema, topic.MessageLogTable(alertsTopic.Id))
	var status *string
	err := ds.Pool.QueryRow(ctx, sql, messageKey).Scan(&status)
	must(err)
	if status == nil {
		return ""
	}
	return *status
}

// waitDelivered returns once the job group's delivery log holds the request
// at the given status.
func waitDelivered(ctx context.Context, messageId int64, status string) {
	sql := fmt.Sprintf(`SELECT COUNT(*) FROM %s.%s WHERE consumer_group_id = %d AND message_id = %d AND status = '%s';`, ds.Schema, topic.DeliveryLogTable(schedulesTopic.Id), jobGroup, messageId, status)

	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		if scalarInt64(ctx, sql) >= 1 {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	die("timed out waiting for: " + sql)
}

func scalarInt64(ctx context.Context, sql string, args ...any) int64 {
	var value int64
	must(ds.Pool.QueryRow(ctx, sql, args...).Scan(&value))
	return value
}

func exec(ctx context.Context, sql string, args ...any) {
	_, err := ds.Pool.Exec(ctx, sql, args...)
	must(err)
}

// --- capture logger ---

// captureLogger records every line so the register-time pass can be counted
// by code, alert, and owner.
type captureLogger struct {
	mu    sync.Mutex
	lines []map[string]any
}

func newCaptureLogger() *captureLogger {
	return &captureLogger{}
}

func (c *captureLogger) record(args []any) {
	fields := map[string]any{}
	for i := 0; i+1 < len(args); i += 2 {
		if key, ok := args[i].(string); ok {
			fields[key] = args[i+1]
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lines = append(c.lines, fields)
}

func (c *captureLogger) DebugContext(ctx context.Context, message string, args ...any) {
	c.record(args)
}

func (c *captureLogger) InfoContext(ctx context.Context, message string, args ...any) {
	c.record(args)
}

func (c *captureLogger) WarnContext(ctx context.Context, message string, args ...any) {
	c.record(args)
}

func (c *captureLogger) ErrorContext(ctx context.Context, message string, args ...any) {
	c.record(args)
}

// find is every line carrying the code, alert, and owner given.
func (c *captureLogger) find(code string, alertName string, ownerName string) []map[string]any {
	c.mu.Lock()
	defer c.mu.Unlock()
	var matches []map[string]any
	for _, line := range c.lines {
		if line["code"] == code && line["alert"] == alertName && line["owner"] == ownerName {
			matches = append(matches, line)
		}
	}
	return matches
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
