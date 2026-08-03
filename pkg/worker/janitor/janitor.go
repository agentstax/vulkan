package janitor

import (
	"context"
	"errors"
	"fmt"

	"github.com/agentstax/vulkan/pkg/common"
	coredatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/logger"
	topiccontroller "github.com/agentstax/vulkan/pkg/topic/controller"
	"github.com/agentstax/vulkan/pkg/worker"
	"github.com/agentstax/vulkan/pkg/worker/controller"
	"github.com/agentstax/vulkan/pkg/worker/janitor/datastore"
)

const WorkerJanitor = "janitor"

// JanitorFactory is the janitor worker factory: Register claims one live instance
// for a topic's janitor worker row and returns it. Register is callable once
// per row to run -- each call claims its own instance.
type JanitorFactory struct {
	Config *JanitorConfig
	Logger logger.Logger // copied from Config.Logger at construction

	workers   *controller.WorkerController
	topics    *topiccontroller.TopicController
	datastore *datastore.JanitorDatastore
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewJanitorFactory(ds *coredatastore.PostgresDatastore, cfg *JanitorConfig) (*JanitorFactory, error) {
	if ds == nil {
		return nil, errors.New("datastore must not be nil")
	}
	if cfg == nil {
		cfg = &JanitorConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	workers, err := controller.NewWorkerController(ds, &controller.ControllerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	topics, err := topiccontroller.NewTopicController(ds, &topiccontroller.ControllerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	janitorDatastore, err := datastore.NewJanitorDatastore(ds, &datastore.JanitorDatastoreConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	return &JanitorFactory{
		Config:    cfg,
		Logger:    cfg.Logger,
		workers:   workers,
		topics:    topics,
		datastore: janitorDatastore,
	}, nil
}

// Name is the worker rows this factory runs.
func (j *JanitorFactory) Name() string {
	return WorkerJanitor
}

// Register claims one live instance, then resolves the topic it sweeps.
// nil = declined (target_instances already filled) -- not an error, try
// again later.
func (j *JanitorFactory) Register(ctx context.Context, workerId int64, owner *common.Owner, metadata any) (worker.Instance, error) {
	claimed, parsed, err := controller.RegisterInstance[janitorMetadata](ctx, j.workers, workerId, owner, common.OwnerTopic, WorkerJanitor, metadata, j.Config.InstanceTTL)
	if err != nil || claimed == nil {
		return nil, err
	}

	current, err := j.topics.GetTopicById(ctx, owner.TopicId)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, fmt.Errorf("topic %d not found -- register it with MessageAdmin.RegisterTopic first", owner.TopicId)
	}

	return newJanitorInstance(j, current, claimed, parsed)
}
