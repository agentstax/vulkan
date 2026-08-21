package producer

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/agentstax/vulkan/pkg/common/diagnostic"
	"github.com/agentstax/vulkan/pkg/common/logging"
	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/metrics"
	iProducer "github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/topic"
)

const pendingGoRoutineEventsLimit = 256

// MetricsProducer is one instance's metrics side-channel: abandoned-routine
// events queue in memory (non-blocking on the consumer claim path), the
// session counters accumulate beside them, and Run flushes both to
// __system.metrics on one tick.
type MetricsProducer struct {
	Config *ProducerConfig
	Logger logging.Logger

	producer     *iProducer.Producer[metrics.GoRoutineEvent]
	measurements *iProducer.Producer[metrics.Measurement]

	// abandoned/cleared events wait here for the next flush tick; capped,
	// drop-on-full -- the queue never blocks a caller or grows unbounded
	goRoutineEventsLock    sync.Mutex
	pendingGoRoutineEvents []*metrics.GoRoutineEvent

	// events dropped at the cap, counted until a flush tick reports them
	droppedGoRoutineEvents atomic.Int64

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

	// the last counter snapshot that landed on the topic
	// this allows skipping a flush when nothing changed
	lastFlushedCounters metrics.SessionCounters
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
	measurements, err := iProducer.NewProducer[metrics.Measurement](ds, &iProducer.ProducerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	return &MetricsProducer{
		Config:       cfg,
		Logger:       cfg.Logger,
		producer:     p,
		measurements: measurements,
	}, nil
}

// Run produces the session's metrics until ctx cancels, then returns nil:
// each SessionFlushRate tick flushes the queued abandoned/cleared events
// and the session counters. No last flush on cancel, the stopped log line
// carries the final totals. Each call registers its own producer instances,
// so Run is callable again after it returns.
func (p *MetricsProducer) Run(ctx context.Context, group string, topicName string, version topic.SchemaVersion, sessionId string) error {
	events, err := p.producer.Register(ctx, metrics.TopicName, topic.SchemaVersion(1))
	if err != nil {
		return err
	}
	measurements, err := p.measurements.Register(ctx, metrics.TopicName, topic.SchemaVersion(1))
	if err != nil {
		return err
	}

	attributes := map[string]string{
		"group":   group,
		"topic":   topicName,
		"version": strconv.FormatInt(int64(version), 10),
		"session": sessionId,
	}

	// a new Run is a new session: its flushes compare against this
	// session's zero, not the previous session's totals
	p.lastFlushedCounters = metrics.SessionCounters{}

	ticker := time.NewTicker(p.Config.SessionFlushRate)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// pending events are dropped -- metrics never hold up a shutdown
			return nil
		case <-ticker.C:
			p.flushGoRoutineEvents(ctx, events)
			p.flushSessionCounters(ctx, measurements, attributes)
		}
	}
}

// flushGoRoutineEvents drains the queued events into one batch. Every drop
// -- events past the cap since the last tick, a batch that could not land
// (dropped, not requeued) -- is reported on the declared VK0052 line.
func (p *MetricsProducer) flushGoRoutineEvents(ctx context.Context, instance *iProducer.ProducerInstance[metrics.GoRoutineEvent]) {
	p.goRoutineEventsLock.Lock()
	events := p.pendingGoRoutineEvents
	p.pendingGoRoutineEvents = nil
	p.goRoutineEventsLock.Unlock()

	dropped := p.droppedGoRoutineEvents.Swap(0)

	var produceErr error
	if len(events) > 0 {
		produceErr = p.produceGoRoutineEvents(ctx, instance, events)
		if produceErr != nil {
			dropped += int64(len(events))
		}
	}

	// a cancel mid-tick is the shutdown, not a failure -- these drops go
	// unreported like the rest of the queue: metrics never hold up a shutdown
	if dropped == 0 || ctx.Err() != nil {
		return
	}
	if produceErr != nil {
		p.Logger.WarnContext(ctx, metrics.EventGoRoutineEventsDropped.Message, "code", metrics.EventGoRoutineEventsDropped.Code, "dropped_count", dropped, "error", produceErr)
		return
	}
	p.Logger.WarnContext(ctx, metrics.EventGoRoutineEventsDropped.Message, "code", metrics.EventGoRoutineEventsDropped.Code, "dropped_count", dropped)
}

func (p *MetricsProducer) produceGoRoutineEvents(ctx context.Context, instance *iProducer.ProducerInstance[metrics.GoRoutineEvent], events []*metrics.GoRoutineEvent) error {
	items := make([]*iProducer.ProduceItem[metrics.GoRoutineEvent], 0, len(events))
	for _, event := range events {
		item, err := iProducer.NewProduceItem(event, iProducer.ProduceOptions{RoutingKey: metrics.AbandonedRoutineKey(event.TopicId, event.Group)})
		if err != nil {
			return err
		}
		items = append(items, item)
	}

	_, err := instance.ProduceBatch(ctx, items...)
	return err
}

// flushSessionCounters produces the counter totals as one batch; an
// unchanged snapshot produces nothing, and a batch that could not land is
// dropped with a warning -- the next tick carries the newer totals.
func (p *MetricsProducer) flushSessionCounters(ctx context.Context, instance *iProducer.ProducerInstance[metrics.Measurement], attributes map[string]string) {
	counters := p.Snapshot()
	if *counters == p.lastFlushedCounters {
		return
	}

	at := time.Now()
	points := []struct {
		metric *diagnostic.Metric
		value  int64
	}{
		{metrics.MetricSessionClaimed, counters.Claimed},
		{metrics.MetricSessionSuccess, counters.Success},
		{metrics.MetricSessionSuperseded, counters.Superseded},
		{metrics.MetricSessionReady, counters.Ready},
		{metrics.MetricSessionDeferred, counters.Deferred},
		{metrics.MetricSessionDead, counters.Dead},
		{metrics.MetricSessionReclaimed, counters.Reclaimed},
		{metrics.MetricSessionQuarantined, counters.Quarantined},
		{metrics.MetricSessionAbandoned, counters.Abandoned},
		{metrics.MetricSessionLeaseLost, counters.LeaseLost},
	}

	items := make([]*iProducer.ProduceItem[metrics.Measurement], 0, len(points))
	for _, point := range points {
		measurement, err := metrics.NewMeasurement(point.metric.Name, metrics.Kind(point.metric.Kind), float64(point.value), metrics.Unit(point.metric.Unit), attributes, at)
		if err != nil {
			p.Logger.WarnContext(ctx, "could not produce session counters", "group", attributes["group"], "topic", attributes["topic"], "session", attributes["session"], "error", err)
			return
		}
		compaction, err := iProducer.NewCompactionOptions(metrics.MeasurementKey(measurement.Name, measurement.Attributes), 0)
		if err != nil {
			p.Logger.WarnContext(ctx, "could not produce session counters", "group", attributes["group"], "topic", attributes["topic"], "session", attributes["session"], "error", err)
			return
		}
		item, err := iProducer.NewProduceItem(measurement, iProducer.ProduceOptions{
			RoutingKey: measurement.Name,
			Compaction: compaction,
		})
		if err != nil {
			p.Logger.WarnContext(ctx, "could not produce session counters", "group", attributes["group"], "topic", attributes["topic"], "session", attributes["session"], "error", err)
			return
		}
		items = append(items, item)
	}

	if _, err := instance.ProduceBatch(ctx, items...); err != nil {
		// a cancel mid-produce is the shutdown, not a failure
		if ctx.Err() == nil {
			p.Logger.WarnContext(ctx, "could not produce session counters", "group", attributes["group"], "topic", attributes["topic"], "session", attributes["session"], "error", err)
		}
		return
	}
	p.lastFlushedCounters = *counters
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

func (p *MetricsProducer) enqueue(event *metrics.GoRoutineEvent) {
	p.goRoutineEventsLock.Lock()
	defer p.goRoutineEventsLock.Unlock()

	// a full queue drops the event -- counted here, reported by the next
	// flush tick, never logged per drop (the cap exists for storms)
	if len(p.pendingGoRoutineEvents) == pendingGoRoutineEventsLimit {
		p.droppedGoRoutineEvents.Add(1)
		return
	}
	p.pendingGoRoutineEvents = append(p.pendingGoRoutineEvents, event)
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

// ResetCounters zeroes the session counters -- a new session's series must
// start from its own zero.
func (p *MetricsProducer) ResetCounters() {
	p.claimed.Store(0)
	p.success.Store(0)
	p.superseded.Store(0)
	p.ready.Store(0)
	p.deferred.Store(0)
	p.dead.Store(0)
	p.reclaimed.Store(0)
	p.quarantined.Store(0)
	p.abandoned.Store(0)
	p.leaseLost.Store(0)
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
