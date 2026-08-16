package manager

import (
	"errors"
	"fmt"

	coredatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/worker"
	"github.com/agentstax/vulkan/pkg/worker/controller"
)

const WorkerManager = "manager"

// A manager execution keeps one running execution per worker row on the
// owner's chain, spawned through the provisioners it was given. Manager rows
// carry no instance target -- the spawned workers' own claims arbitrate who
// runs what, so any number of processes reconcile the same chain safely.
type ManagerDefinition struct {
	Config *ManagerConfig
	Logger logger.Logger

	workers      *controller.WorkerController
	provisioners map[string]worker.Provisioner // keyed by Name; every discovered worker row spawns through the provisioner whose Name matches
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewManagerDefinition(ds *coredatastore.PostgresDatastore, cfg *ManagerConfig, provisioners ...worker.Provisioner) (*ManagerDefinition, error) {
	if ds == nil {
		return nil, errors.New("datastore must not be nil")
	}
	if len(provisioners) == 0 {
		return nil, errors.New("at least one worker provisioner is required")
	}
	if cfg == nil {
		cfg = &ManagerConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	byName := make(map[string]worker.Provisioner, len(provisioners))
	for i, provisioner := range provisioners {
		if provisioner == nil {
			return nil, fmt.Errorf("provisioner %d must not be nil", i)
		}
		if _, taken := byName[provisioner.Name()]; taken {
			return nil, fmt.Errorf("two provisioners run worker %q", provisioner.Name())
		}
		byName[provisioner.Name()] = provisioner
	}

	workers, err := controller.NewWorkerController(ds, &controller.ControllerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	return &ManagerDefinition{
		Config:       cfg,
		Logger:       cfg.Logger,
		workers:      workers,
		provisioners: byName,
	}, nil
}

func (m *ManagerDefinition) Name() string {
	return WorkerManager
}
