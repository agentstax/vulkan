package manager

import (
	"errors"
	"fmt"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/common/logging"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/worker"
	"github.com/agentstax/vulkan/pkg/worker/controller"
)

const WorkerManager = "manager"

// A manager instance keeps one running execution per worker row on the
// owner's chain, spawned through the provisioners it was given. Manager rows
// carry no instance target -- the spawned workers' own claims arbitrate who
// runs what, so any number of processes reconcile the same chain safely.
type ManagerProvisioner struct {
	Config *ManagerConfig
	Logger logging.Logger

	workers      *controller.WorkerController
	provisioners map[string]worker.Provisioner // keyed by Name; every discovered worker row spawns through the provisioner whose Name matches

	definition *worker.Definition
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewManagerProvisioner(ds *iDatastore.PostgresDatastore, cfg *ManagerConfig, provisioners ...worker.Provisioner) (*ManagerProvisioner, error) {
	if ds == nil {
		return nil, errors.New("datastore must not be nil")
	}
	if len(provisioners) == 0 {
		return nil, errors.New("provisioners must not be empty")
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
		if _, taken := byName[provisioner.Definition().Name]; taken {
			return nil, fmt.Errorf("two provisioners run worker %q", provisioner.Definition().Name)
		}
		byName[provisioner.Definition().Name] = provisioner
	}

	workers, err := controller.NewWorkerController(ds, &controller.ControllerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	definition, err := worker.NewDefinition(WorkerManager, common.OwnerAny, defaultManagerMetadata())
	if err != nil {
		return nil, err
	}
	definition.TargetInstances = worker.NoInstanceTarget

	return &ManagerProvisioner{
		Config:       cfg,
		Logger:       cfg.Logger,
		workers:      workers,
		provisioners: byName,
		definition:   definition,
	}, nil
}

func (d *ManagerProvisioner) Definition() *worker.Definition {
	return d.definition
}
