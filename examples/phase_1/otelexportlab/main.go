package main

// Phase 10 lab: point a REAL OTel Prometheus exporter at the metric.Meter
// MessageConsumerConfig accepts, scrape it over a real HTTP server the way
// Prometheus itself would, and confirm every instrument registered by
// pkg/consumer/metrics shows up on the other end -- proof the integration
// works end-to-end, not just that it compiles against the API.
//
// This is the ONLY place in the repo that imports otel/sdk or a specific
// exporter -- pkg/consumer only ever depends on the otel/metric API package
// (see LEARNING_PLAN.md's Phase 10 "OpenTelemetry metrics integration"
// bullet). Precedent: River's rivercontrib/otelriver keeps the same split.

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"time"

	"github.com/agentstax/vulkan/examples/phase_1/common"
	"github.com/agentstax/vulkan/pkg/concurrency"
	"github.com/agentstax/vulkan/pkg/consumer"
	coredatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	otelprometheus "go.opentelemetry.io/otel/exporters/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

const group = "phase10.otelexportlab"

// every instrument pkg/consumer/metrics registers, by the substring its
// otel->Prometheus-translated name is expected to contain. Deliberately
// checked as one flat list, not scenario-by-scenario -- this lab's only job
// is "did it show up on the wire", not "does the value make sense" (the
// other Phase 10 labs already cover that against the live data).
var wantMetrics = []string{
	"vulkan_consumer_queue_state_head",
	"vulkan_consumer_queue_state_claimed",
	"vulkan_consumer_queue_state_committed",
	"vulkan_consumer_queue_state_backlog",
	"vulkan_consumer_queue_state_inflight",
	"vulkan_consumer_queue_state_ready_exceptions",
	"vulkan_consumer_queue_state_inflight_exceptions",
	"vulkan_consumer_queue_state_dead_exceptions",
	"vulkan_consumer_queue_state_oldest_unacked_age",
	"vulkan_consumer_queue_state_open_leases",
	"vulkan_consumer_abandoned_routines_total",
	"vulkan_consumer_abandoned_routines_outstanding",
	"vulkan_consumer_abandoned_routines_self_clear_latency",
}

func main() {
	ctx := context.Background()

	ds, err := coredatastore.NewPostgresDatastore(ctx, &coredatastore.PostgresConnectionConfig{
		User: "example_user", Pass: "example_password",
		Host: "localhost", Port: 5432, Database: "example_db",
	})
	must(err)

	topicName := fmt.Sprintf("%s.%d", group, time.Now().UnixNano())
	tp, err := topic.Register(ctx, ds, topic.Config{Name: topicName})
	must(err)
	defer func() { must(topic.Destroy(ctx, ds, topicName)) }()

	// ===== real OTel SDK + real Prometheus exporter, wired the way an
	// operator actually would: a MeterProvider backed by a Prometheus
	// Reader, registered against its own registry (not the global
	// DefaultRegisterer, so this lab doesn't pollute process-wide state). =====
	step("wiring a real sdkmetric.MeterProvider + otel/exporters/prometheus Reader")
	registry := prometheus.NewRegistry()
	exporter, err := otelprometheus.New(otelprometheus.WithRegisterer(registry))
	must(err)
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(exporter))
	defer func() { must(provider.Shutdown(ctx)) }()
	meter := provider.Meter("otelexportlab")

	pd := producer.NewProducerDatastore[common.Work](ds, nil)
	wp := producer.NewMessageProducer(tp, pd)
	seed(ctx, wp, 5)

	queue, err := concurrency.NewPressureQueue[consumer.MessageRow](20)
	must(err)
	pool, err := concurrency.NewWorkerPoolLimiter(3)
	must(err)

	wc, err := consumer.NewMessageConsumer[common.Work](group, tp, queue, pool, ds, &consumer.MessageConsumerConfig{
		BatchLimit:       5,
		WorkTimeout:      500 * time.Millisecond,
		WorkTimeoutGrace: 50 * time.Millisecond,
		Meter:            meter, // <- the only line that matters: everything above is exporter plumbing
	})
	must(err)
	must(wc.Register(ctx))

	// drive real activity against every instrument before scraping --
	// AbandonedRoutines' Counter/UpDownCounter/Histogram are synchronous
	// instruments, unlike the QueueState ObservableGauges: they only produce
	// a data point once actually recorded to, so a run that never abandons a
	// goroutine would (correctly) never show them on a scrape. Message 1
	// fails (ready exception), message 2 hangs past WorkTimeout (abandoned
	// goroutine), the rest succeed.
	step("driving real consumer activity so every instrument -- including the synchronous ones -- has a data point")
	calls := 0
	must(wc.CursorClaim(ctx, func(ctx context.Context, work *common.Work) error {
		calls++
		switch calls {
		case 1:
			return fmt.Errorf("artificial failure from -fail-rate")
		case 2:
			time.Sleep(700 * time.Millisecond) // outlives WorkTimeout+Grace -- gets abandoned
			return nil
		default:
			return nil
		}
	}))

	step("waiting for the abandoned goroutine to self-clear so its counters carry a real value")
	deadline := time.Now().Add(5 * time.Second)
	for wc.Metrics.AbandonedRoutines.Snapshot().Outstanding > 0 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}

	// ===== real HTTP scrape, the way Prometheus itself would collect it =====
	step("scraping over a real HTTP server via promhttp.HandlerFor")
	server := httptest.NewServer(promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
	defer server.Close()

	resp, err := http.Get(server.URL)
	must(err)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		die(fmt.Sprintf("scrape returned status %d, want 200", resp.StatusCode))
	}
	body, err := io.ReadAll(resp.Body)
	must(err)

	fmt.Printf("  scraped %d bytes from %s\n", len(body), server.URL)

	missing := []string{}
	for _, name := range wantMetrics {
		if !strings.Contains(string(body), name) {
			missing = append(missing, name)
		} else {
			fmt.Printf("  ✓ found %q on the wire\n", name)
		}
	}
	if len(missing) > 0 {
		die(fmt.Sprintf("missing metrics on the scrape: %v\n\n--- full scrape body ---\n%s", missing, body))
	}

	fmt.Println("\n✅ OTEL EXPORT LAB PASSED")
	fmt.Println("   every instrument pkg/consumer/metrics registers showed up on a real Prometheus scrape --")
	fmt.Println("   the integration works end-to-end, not just against the otel/metric API.")
}

// ---- helpers ----

func seed(ctx context.Context, wp *producer.MessageProducer[common.Work], n int) {
	for range n {
		_, err := wp.ProduceFunc(ctx, func(ctx context.Context, tx producer.Tx, _ uuid.UUID) (*common.Work, error) {
			return common.NewWork(30, "admin@example.com")
		}, producer.ProduceOptions{})
		must(err)
	}
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
