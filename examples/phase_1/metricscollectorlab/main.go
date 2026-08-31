package main

// Metrics collector lab: a full-size collection pass under -race -- the
// collectTopics errgroup fans out topic snapshots under TopicConcurrency,
// every fanned-out topic driving singles and per-group ProduceBatch calls
// against ONE ProducerInstance concurrently. Then the
// pipeline's read half: heads and history through the same admin verbs
// `vulkan metrics list` / `vulkan metrics get` render, and a real
// `vulkan manager run --metrics-address` process scraped over HTTP.
// Self-seeding (6 topics x 2 groups x 5 messages), self-cleaning; expects
// bin/vulkan built by the justfile recipe.

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/agentstax/vulkan/examples/phase_1/common"
	iCommon "github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/consumergroup"
	consumergroupcontroller "github.com/agentstax/vulkan/pkg/consumergroup/controller"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/metrics"
	"github.com/agentstax/vulkan/pkg/metrics/collector"
	vulkan "github.com/agentstax/vulkan/pkg/vulkan"
	"github.com/agentstax/vulkan/pkg/worker"
	workercontroller "github.com/agentstax/vulkan/pkg/worker/controller"
)

const (
	databaseURL      = "postgres://example_user:example_password@localhost:5432/example_db"
	topicCount       = 6
	groupsPerTopic   = 2
	messagesPerTopic = 5
	collectorRate    = 200 * time.Millisecond
	metricsAddress   = "127.0.0.1:19565"
)

var groupMetricNames = []string{
	metrics.MetricCursorHead,
	metrics.MetricCursorClaimed,
	metrics.MetricCursorCommitted,
	metrics.MetricCursorBacklog,
	metrics.MetricCursorInflight,
	metrics.MetricReadyExceptions,
	metrics.MetricInflightExceptions,
	metrics.MetricDeferredExceptions,
	metrics.MetricDeadExceptions,
	metrics.MetricOldestUnresolvedAge,
	metrics.MetricOpenLeases,
	metrics.MetricAbandonedOutstanding,
	metrics.MetricAbandonedTotal,
	metrics.MetricAbandonedSelfClearLatencyAvg,
}

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
	run := time.Now().UnixNano()

	ds, err := iDatastore.NewPostgresDatastore(ctx, "example_user", "localhost", "example_db", &iDatastore.PostgresConnectionConfig{Pass: "example_password"})
	must(err)
	defer ds.Close()

	client, err := vulkan.NewClient(ds, &vulkan.ClientConfig{AllowDestroy: true})
	must(err)

	step("seed 6 topics x 2 groups x 5 messages -- more topics than TopicConcurrency")
	consumers, err := consumergroupcontroller.NewConsumerGroupController(ds, nil)
	must(err)

	topicNames := make([]string, 0, topicCount)
	groupNames := make([]string, 0, groupsPerTopic)
	for g := range groupsPerTopic {
		groupNames = append(groupNames, fmt.Sprintf("metricscollectorlab.%c", 'a'+g))
	}
	for t := range topicCount {
		name := fmt.Sprintf("metricscollectorlab.%d.%d", run, t)
		registered, err := client.RegisterTopic(ctx, name, &vulkan.TopicConfig{})
		must(err)
		topicNames = append(topicNames, name)
		defer func() {
			must(client.Topic(name).Destroy(ctx, &vulkan.DestroyOptions{Force: true}))
		}()

		for _, group := range groupNames {
			_, err := consumers.RegisterGroup(ctx, registered.Id, group, consumergroup.Beginning())
			must(err)
		}

		instance, err := client.RegisterProducer[common.Work](ctx, name, nil)
		must(err)
		for range messagesPerTopic {
			work, err := common.NewWork(30, "admin@example.com")
			must(err)
			_, err = instance.Produce(ctx, work, nil)
			must(err)
		}
	}

	step("claim the real metrics_collector worker at a fast poll rate")
	system, err := client.System().Get(ctx)
	must(err)
	systemOwner, err := iCommon.NewSystemOwner(system.Id)
	must(err)
	workers, err := workercontroller.NewWorkerController(ds, nil)
	must(err)
	row, err := workers.GetWorker(ctx, collector.WorkerMetricsCollector, systemOwner)
	must(err)

	provisioner, err := collector.NewMetricsCollectorProvisioner(ds, &collector.MetricsCollectorConfig{
		TopicConcurrency: 4,
	})
	must(err)

	// a crashed earlier run's claim lingers until its InstanceTTL expires --
	// retry past it instead of dying
	row.Metadata = map[string]any{"poll_rate": int64(collectorRate)}

	var execution worker.Execution
	deadline := time.Now().Add(60 * time.Second)
	for {
		execution, err = provisioner.Provision(ctx, row)
		must(err)
		if execution != nil {
			break
		}
		if time.Now().After(deadline) {
			die("metrics collector declined the instance for 60s -- is another claimant running?")
		}
		time.Sleep(time.Second)
	}

	runCtx, cancel := context.WithCancel(ctx)
	collectorDone := make(chan error, 1)
	go func() { collectorDone <- execution.Run(runCtx) }()

	step("wait for full head coverage: fleet + schedules + every lab topic and group")
	expected := map[string]bool{
		metrics.MeasurementKey(metrics.MetricUnclaimedWorkers, nil):   false,
		metrics.MeasurementKey(metrics.MetricOldestUnclaimedAge, nil): false,
		metrics.MeasurementKey(metrics.MetricFailingWorkers, nil):     false,
		metrics.MeasurementKey(metrics.MetricOverdueSchedules, nil):   false,
		metrics.MeasurementKey(metrics.MetricOldestDueAge, nil):       false,
		metrics.MeasurementKey(metrics.MetricSuspendedSchedules, nil): false,
		metrics.MeasurementKey(metrics.MetricActiveAlerts, nil):       false,
		metrics.MeasurementKey(metrics.MetricResolvedAlerts, nil):     false,
	}
	for _, topicName := range topicNames {
		expected[metrics.MeasurementKey(metrics.MetricTopicCompacted, map[string]string{
			"topic": topicName,
		})] = false
		for _, group := range groupNames {
			for _, name := range groupMetricNames {
				expected[metrics.MeasurementKey(name, map[string]string{
					"group": group, "topic": topicName,
				})] = false
			}
		}
	}
	var heads []*vulkan.MessageData[metrics.Measurement]
	must(waitFor(30*time.Second, func() (bool, error) {
		heads, err = client.ListMeasurements(ctx)
		if err != nil {
			return false, err
		}
		covered := 0
		for _, head := range heads {
			if _, ok := expected[head.MessageKey]; ok {
				expected[head.MessageKey] = true
			}
		}
		for _, seen := range expected {
			if seen {
				covered++
			}
		}
		return covered == len(expected), nil
	}))
	fmt.Printf("  ✓ all %d expected series present (%d heads total)\n", len(expected), len(heads))

	step("head values match the seeded state -- nothing consumed yet")
	byKey := make(map[string]*metrics.Measurement, len(heads))
	for _, head := range heads {
		byKey[head.MessageKey] = head.Message
		if head.Message.Attributes["topic"] == metrics.TopicName {
			die(fmt.Sprintf("measurement %s measures __system.metrics -- exclusion broken", head.MessageKey))
		}
	}
	for _, topicName := range topicNames {
		assertValue(byKey, metrics.MetricTopicCompacted, map[string]string{
			"topic": topicName,
		}, 0)
		for _, group := range groupNames {
			attributes := map[string]string{"group": group, "topic": topicName}
			assertValue(byKey, metrics.MetricCursorHead, attributes, messagesPerTopic)
			assertValue(byKey, metrics.MetricCursorBacklog, attributes, messagesPerTopic)
			assertValue(byKey, metrics.MetricCursorClaimed, attributes, 0)
			assertValue(byKey, metrics.MetricDeadExceptions, attributes, 0)
		}
	}
	fmt.Printf("  ✓ compacted=0, head=%d, backlog=%d, claimed=0, dead=0 across %d groups\n",
		messagesPerTopic, messagesPerTopic, topicCount*groupsPerTopic)

	step("history accumulates under the head -- one row per collection pass")
	historyKey := metrics.MeasurementKey(metrics.MetricCursorBacklog, map[string]string{
		"group": groupNames[0], "topic": topicNames[0],
	})
	must(waitFor(10*time.Second, func() (bool, error) {
		history, err := client.ListMeasurementMessages(ctx, historyKey, 10)
		if err != nil {
			return false, err
		}
		return len(history) >= 2, nil
	}))
	fmt.Println("  ✓ >= 2 retained rows for one series key")

	cancel()
	must(<-collectorDone)

	step("vulkan manager run --metrics-address serves the heads as Prometheus text")
	manager := exec.Command("./bin/vulkan", "manager", "run",
		"--metrics-address", metricsAddress,
		"--database-url", databaseURL,
	)
	manager.Stderr = os.Stderr
	must(manager.Start())

	var scrape string
	must(waitFor(15*time.Second, func() (bool, error) {
		response, err := http.Get("http://" + metricsAddress + "/metrics")
		if err != nil {
			return false, nil // not listening yet
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		if err != nil {
			return false, err
		}
		scrape = string(body)
		return response.StatusCode == http.StatusOK, nil
	}))

	for _, series := range []string{
		"vulkan_worker_state_unclaimed_workers ",
		"vulkan_schedule_state_overdue ",
		fmt.Sprintf("vulkan_consumer_cursor_backlog{group=%q,topic=%q} %d", groupNames[0], topicNames[0], messagesPerTopic),
		fmt.Sprintf("vulkan_topic_state_compacted{topic=%q} 0", topicNames[topicCount-1]),
	} {
		if !strings.Contains(scrape, series) {
			die(fmt.Sprintf("scrape missing %q", series))
		}
		fmt.Printf("  ✓ %s\n", strings.TrimRight(series, " "))
	}

	must(manager.Process.Signal(syscall.SIGTERM))
	must(manager.Wait())
	fmt.Println("  ✓ manager process exited cleanly on SIGTERM")

	fmt.Println("\n✅ METRICS COLLECTOR LAB PASSED")
	return nil
}

// ---- helpers ----

func assertValue(byKey map[string]*metrics.Measurement, name string, attributes map[string]string, want float64) {
	key := metrics.MeasurementKey(name, attributes)
	head, ok := byKey[key]
	if !ok {
		die(fmt.Sprintf("no head for %s", key))
	}
	if head.Value != want {
		die(fmt.Sprintf("%s: got %v, want %v", key, head.Value, want))
	}
}

func waitFor(timeout time.Duration, cond func() (bool, error)) error {
	deadline := time.Now().Add(timeout)
	for {
		ok, err := cond()
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for condition")
		}
		time.Sleep(100 * time.Millisecond)
	}
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
