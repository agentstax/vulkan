package janitor

import (
	"errors"

	"github.com/agentstax/vulkan/pkg/common"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	topiccontroller "github.com/agentstax/vulkan/pkg/topic/controller"
	janitorcontroller "github.com/agentstax/vulkan/pkg/topic/janitor/controller"
	"github.com/agentstax/vulkan/pkg/worker"
	"github.com/agentstax/vulkan/pkg/worker/controller"
)

const WorkerJanitor = "janitor"

type JanitorProvisioner struct {
	Config *JanitorConfig
	Logger common.Logger

	workers    *controller.WorkerController
	topics     *topiccontroller.TopicController
	controller *janitorcontroller.JanitorController

	definition *worker.Definition
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewJanitorProvisioner(ds *iDatastore.PostgresDatastore, cfg *JanitorConfig) (*JanitorProvisioner, error) {
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

	definition, err := worker.NewDefinition(WorkerJanitor, common.OwnerTopic, defaultJanitorMetadata())
	if err != nil {
		return nil, err
	}

	return &JanitorProvisioner{
		Config:     cfg,
		Logger:     cfg.Logger,
		workers:    workers,
		topics:     topics,
		controller: janitorController,
		definition: definition,
	}, nil
}

func (d *JanitorProvisioner) Definition() *worker.Definition {
	return d.definition
}
