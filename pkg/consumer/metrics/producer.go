package metrics

import (
	"context"
	"time"

	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/metrics"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/topic"
)

const abandonedEventsBufferSize = 256

// uses queue / drain logic to be non-blocking on consumer claim path
type MetricEventProducer struct {
	producer *producer.Producer[GoRoutineEvent]
	events   chan *GoRoutineEvent
	logger   logger.Logger
}

func NewMetricEventProducer(ds *datastore.PostgresDatastore, cfg *MetricEventConfig) (*MetricEventProducer, error) {
	if cfg == nil {
		cfg = &MetricEventConfig{}
	}

	p, err := producer.NewProducer[GoRoutineEvent](ds, &producer.ProducerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	return &MetricEventProducer{
		producer: p,
		events:   make(chan *GoRoutineEvent, abandonedEventsBufferSize),
		logger:   cfg.Logger,
	}, nil
}

// Run produces queued events until ctx cancels, then returns nil. Each call
// registers its own producer instance, so Run is callable again after it
// returns.
func (e *MetricEventProducer) Run(ctx context.Context) error {
	instance, err := e.producer.Register(ctx, metrics.TopicName, topic.SchemaVersion(1))
	if err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			// queued events are dropped -- metrics never hold up a shutdown
			return nil
		case event := <-e.events:
			e.produce(ctx, instance, event)
		}
	}
}

func (e *MetricEventProducer) Add(ctx context.Context, topicId int64, group string, messageId int64, attempt int) {
	e.enqueue(NewGoRoutineEvent(metrics.EventAbandoned, topicId, group, messageId, attempt, time.Now()))
}

func (e *MetricEventProducer) Remove(ctx context.Context, topicId int64, group string, messageId int64, attempt int) {
	e.enqueue(NewGoRoutineEvent(metrics.EventCleared, topicId, group, messageId, attempt, time.Now()))
}

func (e *MetricEventProducer) enqueue(event *GoRoutineEvent) {
	if e == nil {
		return
	}

	select {
	case e.events <- event:
	default:
	}
}

func (e *MetricEventProducer) produce(ctx context.Context, instance *producer.ProducerInstance[GoRoutineEvent], event *GoRoutineEvent) {
	routingKey := metrics.AbandonedRoutineKey(event.TopicId, event.Group)

	if _, err := instance.Produce(ctx, event, producer.ProduceOptions{RoutingKey: routingKey}); err != nil {
		e.logger.WarnContext(ctx, "abandoned event produce failed", "group", event.Group, "topic_id", event.TopicId, "type", event.EventType, "err", err)
	}
}
