package vulkan

// Package vulkan is the one client over a Postgres pool: the assemblers built
// once, the ambient config held once, every verb delegating to the package
// that owns it.

import (
	"context"
	"errors"

	"github.com/agentstax/vulkan/pkg/admin"
	"github.com/agentstax/vulkan/pkg/alert"
	"github.com/agentstax/vulkan/pkg/common/logging"
	compactioncontroller "github.com/agentstax/vulkan/pkg/compaction/controller"
	consumergroupcontroller "github.com/agentstax/vulkan/pkg/consume/controller"
	"github.com/agentstax/vulkan/pkg/consumer"
	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/metrics"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/scheduler"
	"github.com/agentstax/vulkan/pkg/systemmanager"
	"github.com/jackc/pgx/v5/pgxpool"
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

// NewClient builds every assembler over pool and pings it once, so a wrong
// address or credential fails here instead of at the first query. The pool
// stays the caller's -- vulkan never closes it. cfg may be nil or a sparse
// struct.
func NewClient(ctx context.Context, pool *pgxpool.Pool, cfg *ClientConfig) (*Client, error) {
	if pool == nil {
		return nil, errors.New("pool must not be nil")
	}
	if cfg == nil {
		cfg = &ClientConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	ds, err := datastore.NewPostgresDatastore(ctx, pool, &datastore.PostgresDatastoreConfig{Schema: cfg.Schema})
	if err != nil {
		return nil, err
	}

	// bind logger args with schema
	// set to local var don't overwrite cfg.Logger otherwise multiple
	// NewClient calls add multiple schema args
	logger := logging.NewPipelineLogger(cfg.Logger, &logging.PipelineLoggerConfig{Args: []any{"schema", ds.Schema}})

	messageAdmin, err := admin.NewMessageAdmin(ds, &admin.MessageAdminConfig{
		AllowDestroy: cfg.AllowDestroy,
		Logger:       logger,
		Retry:        cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	messageScheduler, err := scheduler.NewScheduler(ds, &scheduler.SchedulerConfig{
		Logger: logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	systemManager, err := systemmanager.NewSystemManager(ds, &systemmanager.SystemManagerConfig{
		Logger: logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	groupController, err := consumergroupcontroller.NewConsumerGroupController(ds, &consumergroupcontroller.ControllerConfig{
		Logger: logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	compactionController, err := compactioncontroller.NewCompactionController(ds, &compactioncontroller.ControllerConfig{
		Logger: logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	return &Client{
		Config:    cfg,
		Logger:    logger,
		ds:        ds,
		admin:     messageAdmin,
		scheduler: messageScheduler,
		manager:   systemManager,
		groups:    groupController,
		heads:     compactionController,
	}, nil
}

// Datastore returns the handle NewClient built over the pool, for the paths
// vulkan's own verbs do not cover -- otelvulkan's exporter, a diagnostic
// query. One client is one datastore, so its Schema is always the client's.
func (c *Client) Datastore() *datastore.PostgresDatastore {
	return c.ds
}

// RegisterConsumer resolves the named topic and registers the consumer
// group on it, returning an instance that consumes Message from it.
// cfg is the group's declaration -- nil or sparse for the defaults, with
// cfg.Bindings the group's full pattern set (nil = the whole topic).
// ctx bounds only this call's I/O; the instance's lifetime is Consume's ctx.
func (c *Client) RegisterConsumer[Message Versioned](ctx context.Context, consumerGroup string, topicName string, cfg *ConsumerConfig) (*ConsumerInstance[Message], error) {
	messageConsumer, err := consumer.NewConsumer(c.ds, toConsumerConfig(cfg, c.Config.Retry, c.Logger))
	if err != nil {
		return nil, err
	}

	var bindings []string
	if cfg != nil {
		bindings = cfg.Bindings
	}
	instance, err := messageConsumer.Register[Message](ctx, consumerGroup, topicName, bindings)
	if err != nil {
		return nil, err
	}
	return newConsumerInstance(instance, c.manager, !c.Config.DisableManager)
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

// RegisterSchedule declares the schedule spec names on its target topic and
// returns its handle. The newest declaration wins. cfg may be nil or
// sparse.
func (c *Client) RegisterSchedule[Message Versioned](ctx context.Context, spec ScheduleSpec, payload *Message, cfg *ScheduleConfig) (*Schedule, error) {
	if _, err := c.scheduler.Register[Message](ctx, spec.Name, spec.Cron, spec.Topic, payload, cfg); err != nil {
		return nil, err
	}
	return c.Schedule(spec.Name), nil
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

func (c *Client) ListBindingDeclarations(ctx context.Context) ([]*BindingDeclaration, error) {
	return c.admin.ListBindingDeclarations(ctx)
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
// row in the deployment until ctx cancels, then returns nil. Safe to run
// N-way -- the row admits one reconcile loop at a time.
func (c *Client) RunManager(ctx context.Context) error {
	return c.manager.Run(ctx)
}
