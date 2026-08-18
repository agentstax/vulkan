package janitor

import (
	"errors"

	"github.com/agentstax/vulkan/pkg/common"
	coredatastore "github.com/agentstax/vulkan/pkg/datastore"
	topiccontroller "github.com/agentstax/vulkan/pkg/topic/controller"
	"github.com/agentstax/vulkan/pkg/worker/controller"
	"github.com/agentstax/vulkan/pkg/worker/janitor/datastore"
)

const WorkerJanitor = "janitor"

type JanitorDefinition struct {
	Config *JanitorConfig
	Logger common.Logger

	workers   *controller.WorkerController
	topics    *topiccontroller.TopicController
	datastore *datastore.JanitorDatastore
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewJanitorDefinition(ds *coredatastore.PostgresDatastore, cfg *JanitorConfig) (*JanitorDefinition, error) {
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

	return &JanitorDefinition{
		Config:    cfg,
		Logger:    cfg.Logger,
		workers:   workers,
		topics:    topics,
		datastore: janitorDatastore,
	}, nil
}

func (j *JanitorDefinition) Name() string {
	return WorkerJanitor
}
