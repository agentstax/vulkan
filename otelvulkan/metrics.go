// Package otelvulkan exposes a Vulkan deployment's measurements over
// OpenTelemetry: each series' newest measurement on __system.metrics becomes an
// observable instrument. Metrics feeds any otel meter you own; Exporter
// builds on it to serve a Prometheus /metrics endpoint; MetricsProducer and
// MetricsConsumer publish and read your own measurements on the same topic.
package otelvulkan

import (
	"context"
	"errors"
	"maps"
	"sync"

	"github.com/agentstax/vulkan/pkg/common/logging"
	compactioncontroller "github.com/agentstax/vulkan/pkg/compaction/controller"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/metrics"
	"github.com/agentstax/vulkan/pkg/migrate"
	"github.com/agentstax/vulkan/pkg/topic"
	topiccontroller "github.com/agentstax/vulkan/pkg/topic/controller"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const meterScopeName = "github.com/agentstax/vulkan/otelvulkan"

// Metrics registers the measurements on an otel meter: one observable instrument
// per metric name, whose values are each series' newest measurement, read live
// whenever the meter's reader collects. Pass your own meter through
// MetricsConfig.Meter to feed your own pipeline.
type Metrics struct {
	Config *MetricsConfig
	Logger logging.Logger

	topics *topiccontroller.TopicController
	heads  *compactioncontroller.CompactionController[metrics.Measurement]
	meter  metric.Meter

	// instruments can only be created outside the observation callback, so
	// RegisterMetricInstruments creates instruments for names it hasn't
	// seen; the mutex orders concurrent callers doing that
	mutex        sync.Mutex
	topicId      int64
	instruments  map[string]metric.Float64Observable
	registration metric.Registration
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewMetrics(ds *iDatastore.PostgresDatastore, cfg *MetricsConfig) (*Metrics, error) {
	if ds == nil {
		return nil, errors.New("datastore must not be nil")
	}
	if cfg == nil {
		cfg = &MetricsConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	topics, err := topiccontroller.NewTopicController(ds, &topiccontroller.ControllerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	heads, err := compactioncontroller.NewCompactionController[metrics.Measurement](ds, &compactioncontroller.ControllerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	return &Metrics{
		Config:      cfg,
		Logger:      cfg.Logger,
		topics:      topics,
		heads:       heads,
		meter:       cfg.Meter,
		instruments: map[string]metric.Float64Observable{},
	}, nil
}

// RegisterMetricInstruments creates one observable instrument per measurement
// name currently on the topic, skipping names already registered. Run it
// before the first collection, and again whenever new names may have
// appeared -- the Exporter runs it per scrape.
// Returns ErrTopicNotFound until RegisterSystem has run.
func (m *Metrics) RegisterMetricInstruments(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, m.Config.CollectTimeout)
	defer cancel()

	m.mutex.Lock()
	defer m.mutex.Unlock()

	topicId, err := m.resolveTopicId(ctx)
	if err != nil {
		return err
	}

	rows, err := m.heads.ListHeads(ctx, topicId)
	if err != nil {
		return err
	}

	created := false
	for _, row := range rows {
		if _, seen := m.instruments[row.Message.Name]; seen {
			continue
		}
		instrument, err := m.newInstrument(row.Message)
		if err != nil {
			return err
		}
		m.instruments[row.Message.Name] = instrument
		created = true
	}
	if !created {
		return nil
	}

	// the callback only fires for instruments it was registered against, so
	// a new name replaces the whole registration
	observed := make([]metric.Observable, 0, len(m.instruments))
	for _, instrument := range m.instruments {
		observed = append(observed, instrument)
	}
	if m.registration != nil {
		if err := m.registration.Unregister(); err != nil {
			return err
		}
	}
	registration, err := m.meter.RegisterCallback(m.observe, observed...)
	if err != nil {
		return err
	}
	m.registration = registration
	return nil
}

// a counter measurement carries a running total, so it maps to the monotonic
// observable; everything else is a point-in-time gauge
func (m *Metrics) newInstrument(measurement *metrics.Measurement) (metric.Float64Observable, error) {
	if measurement.Kind == metrics.KindCounter {
		return m.meter.Float64ObservableCounter(measurement.Name, metric.WithUnit(string(measurement.Unit)))
	}
	return m.meter.Float64ObservableGauge(measurement.Name, metric.WithUnit(string(measurement.Unit)))
}

// observe runs inside every collection the meter's reader drives. A
// collection may carry no deadline of its own, so the head read gets an
// explicit bound.
func (m *Metrics) observe(ctx context.Context, observer metric.Observer) error {
	ctx, cancel := context.WithTimeout(ctx, m.Config.CollectTimeout)
	defer cancel()

	m.mutex.Lock()
	topicId := m.topicId
	instruments := make(map[string]metric.Float64Observable, len(m.instruments))
	maps.Copy(instruments, m.instruments)
	m.mutex.Unlock()

	rows, err := m.heads.ListHeads(ctx, topicId)
	if err != nil {
		return err
	}

	for _, row := range rows {
		instrument, seen := instruments[row.Message.Name]
		if !seen {
			// name first published mid-collection -- the next
			// RegisterMetricInstruments creates its instrument
			continue
		}
		observer.ObserveFloat64(instrument, row.Message.Value, metric.WithAttributes(toAttributes(row.Message.Attributes)...))
	}
	return nil
}

// resolveTopicId caches __system.metrics's id after the first success; the
// caller holds the mutex.
func (m *Metrics) resolveTopicId(ctx context.Context) (int64, error) {
	if m.topicId != 0 {
		return m.topicId, nil
	}
	found, err := m.topics.Get(ctx, metrics.TopicName, topic.SchemaVersion(1))
	if err != nil {
		return 0, err
	}
	if found == nil {
		return 0, migrate.ErrNotRegistered.With("topic", metrics.TopicName)
	}
	m.topicId = found.Id
	return found.Id, nil
}

func toAttributes(attributes map[string]string) []attribute.KeyValue {
	pairs := make([]attribute.KeyValue, 0, len(attributes))
	for key, value := range attributes {
		pairs = append(pairs, attribute.String(key, value))
	}
	return pairs
}
