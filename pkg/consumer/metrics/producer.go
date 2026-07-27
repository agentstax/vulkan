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

func NewMetricEventProducer(group string, ds *datastore.PostgresDatastore, cfg *MetricEventConfig) (*MetricEventProducer, error) {
	// topic name == __system.metrics
	p, err := producer.NewProducer[GoRoutineEvent](metrics.TopicName, topic.SchemaVersion(1), ds, &producer.ProducerConfig{
		Logger:                  cfg.Logger,
		Retry:                   cfg.Retry,
		DisableGracefulShutdown: cfg.DisableGracefulShutdown,
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

func (e *MetricEventProducer) Register(ctx context.Context) error {
	if err := e.producer.Register(ctx); err != nil {
		return err
	}

	go e.drain(ctx)

	return nil
}

func (e *MetricEventProducer) Add(ctx context.Context, topicId int64, group string, messageId int64, attempt int) {
	e.enqueue(NewGoRoutineEvent(EventAbandoned, topicId, group, messageId, attempt, time.Now()))
}

func (e *MetricEventProducer) Remove(ctx context.Context, topicId int64, group string, messageId int64, attempt int) {
	e.enqueue(NewGoRoutineEvent(EventCleared, topicId, group, messageId, attempt, time.Now()))
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

func (e *MetricEventProducer) drain(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			// don't care about graceful shutdown for metrics at this point
			return
		case event := <-e.events:
			e.produce(ctx, event)
		}
	}
}

func (e *MetricEventProducer) produce(ctx context.Context, event *GoRoutineEvent) {
	routingKey := metrics.AbandonedRoutineKey(event.TopicId, event.Group)

	if _, err := e.producer.Produce(ctx, event, producer.ProduceOptions{RoutingKey: routingKey}); err != nil {
		e.logger.WarnContext(ctx, "abandoned event produce failed", "group", event.Group, "topic_id", event.TopicId, "type", event.EventType, "err", err)
	}
}
