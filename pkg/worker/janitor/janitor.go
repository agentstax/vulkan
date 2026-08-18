package janitor

import (
	"errors"

	"github.com/agentstax/vulkan/pkg/common"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	topiccontroller "github.com/agentstax/vulkan/pkg/topic/controller"
	"github.com/agentstax/vulkan/pkg/worker/controller"
	janitorcontroller "github.com/agentstax/vulkan/pkg/worker/janitor/controller"
)

const WorkerJanitor = "janitor"

type JanitorDefinition struct {
	Config *JanitorConfig
	Logger common.Logger

	workers    *controller.WorkerController
	topics     *topiccontroller.TopicController
	controller *janitorcontroller.JanitorController
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewJanitorDefinition(ds *iDatastore.PostgresDatastore, cfg *JanitorConfig) (*JanitorDefinition, error) {
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

	janitorController, err := janitorcontroller.NewJanitorController(ds, &janitorcontroller.ControllerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	return &JanitorDefinition{
		Config:     cfg,
		Logger:     cfg.Logger,
		workers:    workers,
		topics:     topics,
		controller: janitorController,
	}, nil
}

func (j *JanitorDefinition) Name() string {
	return WorkerJanitor
}
