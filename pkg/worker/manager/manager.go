package manager

import (
	"context"
	"errors"
	"fmt"

	"github.com/agentstax/vulkan/pkg/common"
	coredatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/worker"
	"github.com/agentstax/vulkan/pkg/worker/controller"
)

const WorkerManager = "manager"

// ManagerFactory is the manager worker factory: Register claims one live
// instance of the system's manager worker row and returns it; the instance
// keeps one running instance per worker row it discovers, spawned through
// the factories the manager was given.
type ManagerFactory struct {
	Config *ManagerConfig
	Logger logger.Logger // copied from Config.Logger at construction

	workers   *controller.WorkerController
	factories map[string]worker.Factory // keyed by Name; every discovered worker row spawns through the factory whose Name matches
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewManagerFactory(ds *coredatastore.PostgresDatastore, cfg *ManagerConfig, factories ...worker.Factory) (*ManagerFactory, error) {
	if ds == nil {
		return nil, errors.New("datastore must not be nil")
	}
	if len(factories) == 0 {
		return nil, errors.New("at least one worker factory is required")
	}
	if cfg == nil {
		cfg = &ManagerConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	byName := make(map[string]worker.Factory, len(factories))
	for i, factory := range factories {
		if factory == nil {
			return nil, fmt.Errorf("factory %d must not be nil", i)
		}
		if _, taken := byName[factory.Name()]; taken {
			return nil, fmt.Errorf("two factories run worker %q", factory.Name())
		}
		byName[factory.Name()] = factory
	}

	workers, err := controller.NewWorkerController(ds, &controller.ControllerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	return &ManagerFactory{
		Config:    cfg,
		Logger:    cfg.Logger,
		workers:   workers,
		factories: byName,
	}, nil
}

// Name is the worker rows this factory runs.
func (m *ManagerFactory) Name() string {
	return WorkerManager
}

// Register claims one live instance. nil = declined (target_instances
// already filled) -- not an error, try again later.
func (m *ManagerFactory) Register(ctx context.Context, workerId int64, owner *common.Owner, metadata any) (worker.Instance, error) {
	claimed, parsed, err := controller.RegisterInstance[managerMetadata](ctx, m.workers, workerId, owner, common.OwnerSystem, WorkerManager, metadata, m.Config.InstanceTTL)
	if err != nil || claimed == nil {
		return nil, err
	}
	return newManagerInstance(m, claimed, parsed)
}
