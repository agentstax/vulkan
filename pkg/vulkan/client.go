package vulkan

// Package vulkan is the one client over a datastore: the assemblers built
// once, the ambient config held once, every verb delegating to the package
// that owns it.

import (
	"context"
	"errors"

	"github.com/agentstax/vulkan/pkg/admin"
	"github.com/agentstax/vulkan/pkg/alert"
	"github.com/agentstax/vulkan/pkg/common/logging"
	compactioncontroller "github.com/agentstax/vulkan/pkg/compaction/controller"
	"github.com/agentstax/vulkan/pkg/consumer"
	consumergroupcontroller "github.com/agentstax/vulkan/pkg/consumergroup/controller"
	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/metrics"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/scheduler"
	"github.com/agentstax/vulkan/pkg/systemmanager"
)

type Client struct {
	Config *ClientConfig
	Logger logging.Logger

	ds        *datastore.PostgresDatastore
	admin     *admin.MessageAdmin
	scheduler *scheduler.Scheduler
	manager   *systemmanager.SystemManager
	groups    *consumergroupcontroller.ConsumerGroupController
	heads     *compactioncontroller.CompactionController
}

// NewClient wraps ds -- it does not connect, and the caller keeps closing
// the pool. cfg may be nil or a sparse struct.
func NewClient(ds *datastore.PostgresDatastore, cfg *ClientConfig) (*Client, error) {
	if ds == nil {
		return nil, errors.New("datastore must not be nil")
	}
	if cfg == nil {
		cfg = &ClientConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	messageAdmin, err := admin.NewMessageAdmin(ds, &admin.MessageAdminConfig{
		AllowDestroy: cfg.AllowDestroy,
		Logger:       cfg.Logger,
		Retry:        cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	messageScheduler, err := scheduler.NewScheduler(ds, &scheduler.SchedulerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	systemManager, err := systemmanager.NewSystemManager(ds, &systemmanager.SystemManagerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	groupController, err := consumergroupcontroller.NewConsumerGroupController(ds, &consumergroupcontroller.ControllerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	compactionController, err := compactioncontroller.NewCompactionController(ds, &compactioncontroller.ControllerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	return &Client{
		Config:    cfg,
		Logger:    cfg.Logger,
		ds:        ds,
		admin:     messageAdmin,
		scheduler: messageScheduler,
		manager:   systemManager,
		groups:    groupController,
		heads:     compactionController,
	}, nil
}

// RegisterConsumer resolves the named topic and registers the consumer
// group on it, returning an instance that consumes Message from it.
// bindings is the group's full set; nil = the whole topic.
// cfg may be nil or a sparse struct.
// ctx bounds only this call's I/O; the instance's lifetime is Consume's ctx.
func (c *Client) RegisterConsumer[Message Versioned](ctx context.Context, consumerGroup string, topicName string, bindings []string, cfg *ConsumerConfig) (*ConsumerInstance[Message], error) {
	messageConsumer, err := consumer.NewConsumer(c.ds, toConsumerConfig(cfg, c.Config.Retry, c.Logger))
	if err != nil {
		return nil, err
	}
	return messageConsumer.Register[Message](ctx, consumerGroup, topicName, bindings)
}

// RegisterProducer resolves the named topic and returns an instance that
// produces Message to it. cfg may be nil or a sparse struct.
func (c *Client) RegisterProducer[Message Versioned](ctx context.Context, topicName string, cfg *ProducerConfig) (*ProducerInstance[Message], error) {
	messageProducer, err := producer.NewProducer(c.ds, toProducerConfig(cfg, c.Config.Retry, c.Logger))
	if err != nil {
		return nil, err
	}
	return messageProducer.Register[Message](ctx, topicName)
}

// RegisterSchedule declares the schedule named name on the target topic and
// returns its handle. The newest declaration wins. cfg may be nil or
// sparse.
func (c *Client) RegisterSchedule[Message Versioned](ctx context.Context, name string, expression string, topicName string, payload *Message, cfg *ScheduleConfig) (*Schedule, error) {
	if _, err := c.scheduler.Register[Message](ctx, name, expression, topicName, payload, cfg); err != nil {
		return nil, err
	}
	return c.Schedule(name), nil
}

// RegisterTopic declares the named topic, creating its tables on first
// registration. Idempotent; cfg may be nil or sparse.
func (c *Client) RegisterTopic(ctx context.Context, name string, cfg *TopicConfig) (*TopicData, error) {
	return c.admin.RegisterTopic(ctx, name, cfg)
}

// RegisterSystem declares the system's own knobs -- the built-in alert
// schedules. Safe to run on every startup; cfg may be nil.
func (c *Client) RegisterSystem(ctx context.Context, cfg *RegisterSystemConfig) error {
	return c.admin.RegisterSystem(ctx, cfg)
}

func (c *Client) ListTopics(ctx context.Context) ([]*TopicData, error) {
	return c.admin.ListTopics(ctx)
}

func (c *Client) ListSchedules(ctx context.Context) ([]*ScheduleData, error) {
	return c.admin.ListSchedules(ctx)
}

func (c *Client) ListDeclarations(ctx context.Context) ([]*Declaration, error) {
	return c.admin.ListDeclarations(ctx)
}

func (c *Client) ListAlerts(ctx context.Context) ([]*MessageData[alert.Alert], error) {
	return c.admin.ListAlerts(ctx)
}

func (c *Client) ListMeasurements(ctx context.Context) ([]*MessageData[metrics.Measurement], error) {
	return c.admin.ListMeasurements(ctx)
}

func (c *Client) ListMeasurementMessages(ctx context.Context, messageKey string, limit int) ([]*MessageData[metrics.Measurement], error) {
	return c.admin.ListMeasurementMessages(ctx, messageKey, limit)
}

func (c *Client) MigrateTopics(ctx context.Context, targetVersion int64) error {
	return c.admin.MigrateTopics(ctx, targetVersion)
}

// RunManager claims the system's manager row and reconciles every worker
// row in the deployment until ctx cancels. The client holds one
// SystemManager, so a second concurrent run in this process is refused.
func (c *Client) RunManager(ctx context.Context) error {
	return c.manager.Run(ctx)
}
