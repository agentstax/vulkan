package producer

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	"github.com/agentstax/vulkan/pkg/common/logging"
	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/metrics"
	iProducer "github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/topic"
)

const abandonedEventsBufferSize = 256

// MetricsProducer is one instance's metrics side-channel: abandoned-routine
// events queue/drain to __system.metrics (non-blocking on the consumer claim
// path), and the session counters accumulate in memory for Snapshot.
type MetricsProducer struct {
	Logger logging.Logger

	producer *iProducer.Producer[metrics.GoRoutineEvent]
	events   chan *metrics.GoRoutineEvent

	claimed     atomic.Int64
	success     atomic.Int64
	superseded  atomic.Int64
	ready       atomic.Int64
	deferred    atomic.Int64
	dead        atomic.Int64
	reclaimed   atomic.Int64
	quarantined atomic.Int64
	abandoned   atomic.Int64
	leaseLost   atomic.Int64
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewMetricsProducer(ds *datastore.PostgresDatastore, cfg *ProducerConfig) (*MetricsProducer, error) {
	if ds == nil {
		return nil, errors.New("datastore must not be nil")
	}
	if cfg == nil {
		cfg = &ProducerConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	p, err := iProducer.NewProducer[metrics.GoRoutineEvent](ds, &iProducer.ProducerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	return &MetricsProducer{
		Logger:   cfg.Logger,
		producer: p,
		events:   make(chan *metrics.GoRoutineEvent, abandonedEventsBufferSize),
	}, nil
}

// Run produces queued events until ctx cancels, then returns nil. Each call
// registers its own producer instance, so Run is callable again after it
// returns.
func (p *MetricsProducer) Run(ctx context.Context) error {
	instance, err := p.producer.Register(ctx, metrics.TopicName, topic.SchemaVersion(1))
	if err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			// queued events are dropped -- metrics never hold up a shutdown
			return nil
		case event := <-p.events:
			p.produce(ctx, instance, event)
		}
	}
}

// RecordAbandoned queues an abandoned event and counts it -- the counter is
// monotonic, a later clear does not undo it.
func (p *MetricsProducer) RecordAbandoned(topicId int64, group string, messageId int64, attempt int) {
	p.abandoned.Add(1)
	p.enqueue(metrics.NewGoRoutineEvent(metrics.EventAbandoned, topicId, group, messageId, attempt, time.Now()))
}

func (p *MetricsProducer) RecordCleared(topicId int64, group string, messageId int64, attempt int) {
	p.enqueue(metrics.NewGoRoutineEvent(metrics.EventCleared, topicId, group, messageId, attempt, time.Now()))
}

func (p *MetricsProducer) RecordClaimed(count int) {
	p.claimed.Add(int64(count))
}

func (p *MetricsProducer) RecordSuccess(count int) {
	p.success.Add(int64(count))
}

func (p *MetricsProducer) RecordSuperseded(count int) {
	p.superseded.Add(int64(count))
}

func (p *MetricsProducer) RecordReady(count int) {
	p.ready.Add(int64(count))
}

func (p *MetricsProducer) RecordDeferred(count int) {
	p.deferred.Add(int64(count))
}

func (p *MetricsProducer) RecordDead(count int) {
	p.dead.Add(int64(count))
}

func (p *MetricsProducer) RecordReclaimed(count int) {
	p.reclaimed.Add(int64(count))
}

func (p *MetricsProducer) RecordQuarantined(count int) {
	p.quarantined.Add(int64(count))
}

func (p *MetricsProducer) RecordLeaseLost(count int) {
	p.leaseLost.Add(int64(count))
}

// Snapshot reads the session counters. Each counter loads atomically; the set
// is not one consistent cut, which a lifetime-totals read never needs.
func (p *MetricsProducer) Snapshot() *metrics.SessionCounters {
	return &metrics.SessionCounters{
		Claimed:     p.claimed.Load(),
		Success:     p.success.Load(),
		Superseded:  p.superseded.Load(),
		Ready:       p.ready.Load(),
		Deferred:    p.deferred.Load(),
		Dead:        p.dead.Load(),
		Reclaimed:   p.reclaimed.Load(),
		Quarantined: p.quarantined.Load(),
		Abandoned:   p.abandoned.Load(),
		LeaseLost:   p.leaseLost.Load(),
	}
}

func (p *MetricsProducer) enqueue(event *metrics.GoRoutineEvent) {
	select {
	case p.events <- event:
	default:
	}
}

func (p *MetricsProducer) produce(ctx context.Context, instance *iProducer.ProducerInstance[metrics.GoRoutineEvent], event *metrics.GoRoutineEvent) {
	routingKey := metrics.AbandonedRoutineKey(event.TopicId, event.Group)

	if _, err := instance.Produce(ctx, event, iProducer.ProduceOptions{RoutingKey: routingKey}); err != nil {
		p.Logger.WarnContext(ctx, "could not produce abandoned event", "group", event.Group, "topic_id", event.TopicId, "type", event.EventType, "error", err)
	}
}
