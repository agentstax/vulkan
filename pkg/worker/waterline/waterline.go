package waterline

import (
	"context"
	"errors"

	"github.com/agentstax/vulkan/pkg/common"
	coredatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/worker"
	"github.com/agentstax/vulkan/pkg/worker/controller"
	"github.com/agentstax/vulkan/pkg/worker/waterline/datastore"
)

const WorkerWaterline = "waterline"

type WaterlineDefinition struct {
	Config *WaterlineConfig
	Logger logger.Logger

	workers   *controller.WorkerController
	datastore *datastore.WaterlineDatastore
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewWaterlineDefinition(ds *coredatastore.PostgresDatastore, cfg *WaterlineConfig) (*WaterlineDefinition, error) {
	if ds == nil {
		return nil, errors.New("datastore must not be nil")
	}
	if cfg == nil {
		cfg = &WaterlineConfig{}
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

	waterlineDatastore, err := datastore.NewWaterlineDatastore(ds, &datastore.WaterlineDatastoreConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	return &WaterlineDefinition{
		Config:    cfg,
		Logger:    cfg.Logger,
		workers:   workers,
		datastore: waterlineDatastore,
	}, nil
}

func (w *WaterlineDefinition) Name() string {
	return WorkerWaterline
}

// Register claims one live instance. nil = declined (target_instances
// already filled) -- not an error, try again later.
func (w *WaterlineDefinition) Provision(ctx context.Context, workerId int64, owner *common.Owner, metadata any) (worker.Execution, error) {
	parsed, err := controller.ParseMetadata[waterlineMetadata](metadata)
	if err != nil {
		return nil, err
	}
	if err := parsed.Validate(); err != nil {
		return nil, err
	}
	claimed, err := controller.RegisterInstance(ctx, w.workers, workerId, owner, common.OwnerConsumerGroup, WorkerWaterline, w.Config.InstanceTTL)
	if err != nil || claimed == nil {
		return nil, err
	}
	return newWaterlineExecution(w, owner, claimed, parsed)
}
