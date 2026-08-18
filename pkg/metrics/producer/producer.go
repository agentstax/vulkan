package producer

import (
	"context"
	"errors"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/metrics"
	iProducer "github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/topic"
)

const abandonedEventsBufferSize = 256

// uses queue / drain logic to be non-blocking on consumer claim path
type MetricsProducer struct {
	Logger common.Logger

	producer *iProducer.Producer[metrics.GoRoutineEvent]
	events   chan *metrics.GoRoutineEvent
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
func (e *MetricsProducer) Run(ctx context.Context) error {
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

func (e *MetricsProducer) Add(topicId int64, group string, messageId int64, attempt int) {
	e.enqueue(metrics.NewGoRoutineEvent(metrics.EventAbandoned, topicId, group, messageId, attempt, time.Now()))
}

func (e *MetricsProducer) Remove(topicId int64, group string, messageId int64, attempt int) {
	e.enqueue(metrics.NewGoRoutineEvent(metrics.EventCleared, topicId, group, messageId, attempt, time.Now()))
}

func (e *MetricsProducer) enqueue(event *metrics.GoRoutineEvent) {
	select {
	case e.events <- event:
	default:
	}
}

func (e *MetricsProducer) produce(ctx context.Context, instance *iProducer.ProducerInstance[metrics.GoRoutineEvent], event *metrics.GoRoutineEvent) {
	routingKey := metrics.AbandonedRoutineKey(event.TopicId, event.Group)

	if _, err := instance.Produce(ctx, event, iProducer.ProduceOptions{RoutingKey: routingKey}); err != nil {
		e.Logger.WarnContext(ctx, "abandoned event produce failed", "group", event.Group, "topic_id", event.TopicId, "type", event.EventType, "err", err)
	}
}
