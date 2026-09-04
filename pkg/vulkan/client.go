package vulkan

// Package vulkan is the one client over a Postgres pool: registration objects
// built once, ambient config held once, and every verb delegated to the
// package that owns it.

import (
	"context"
	"errors"

	"github.com/agentstax/vulkan/pkg/admin"
	"github.com/agentstax/vulkan/pkg/common/logging"
	"github.com/agentstax/vulkan/pkg/consumer"
	"github.com/agentstax/vulkan/pkg/datastore"
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
	consumer  *consumer.Consumer
	producer  *producer.Producer
	scheduler *scheduler.Scheduler
	manager   *systemmanager.SystemManager
}

// NewClient builds every registration object over pool and pings it once, so a wrong
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

	messageConsumer, err := consumer.NewConsumer(ds)
	if err != nil {
		return nil, err
	}
	messageProducer, err := producer.NewProducer(ds)
	if err != nil {
		return nil, err
	}
	messageScheduler, err := scheduler.NewScheduler(ds)
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

	return &Client{
		Config:    cfg,
		Logger:    logger,
		ds:        ds,
		admin:     messageAdmin,
		consumer:  messageConsumer,
		producer:  messageProducer,
		scheduler: messageScheduler,
		manager:   systemManager,
	}, nil
}

// Datastore returns the handle NewClient built over the pool, for the paths
// vulkan's own verbs do not cover -- otelvulkan's exporter, a diagnostic
// query. One client is one datastore, so its Schema is always the client's.
func (c *Client) Datastore() *datastore.PostgresDatastore {
	return c.ds
}

// InTransaction opens one transaction, runs transactionFunc against it, and
// commits -- the way to publish to multiple targets atomically via ProduceInTx.
//
// It does not retry -- a transient blip or an ambiguous commit failure
// surfaces to you as-is. Wrap your own retry loop around it if you want one;
// only you know what's safe to rerun in your closure. Rerunning the whole
// closure is dedup-safe ONLY under caller-supplied IdempotencyKeys -- unset
// keys mint fresh per call, so a rerun double-publishes.
func (c *Client) InTransaction(ctx context.Context, transactionFunc TransactionFunc) error {
	return datastore.InTransaction(ctx, c.ds, transactionFunc)
}
