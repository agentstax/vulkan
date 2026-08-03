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

// WaterlineFactory is the waterline worker factory: Register claims one live
// instance for a consumer group's waterline worker row and returns it.
// Register is callable once per row to run -- each call claims its own
// instance.
type WaterlineFactory struct {
	Config *WaterlineConfig
	Logger logger.Logger // copied from Config.Logger at construction

	workers   *controller.WorkerController
	datastore *datastore.WaterlineDatastore
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewWaterlineFactory(ds *coredatastore.PostgresDatastore, cfg *WaterlineConfig) (*WaterlineFactory, error) {
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

	return &WaterlineFactory{
		Config:    cfg,
		Logger:    cfg.Logger,
		workers:   workers,
		datastore: waterlineDatastore,
	}, nil
}

// Name is the worker rows this factory runs.
func (w *WaterlineFactory) Name() string {
	return WorkerWaterline
}

// Register claims one live instance. nil = declined (target_instances
// already filled) -- not an error, try again later.
func (w *WaterlineFactory) Register(ctx context.Context, workerId int64, owner *common.Owner, metadata any) (worker.Instance, error) {
	claimed, parsed, err := controller.RegisterInstance[waterlineMetadata](ctx, w.workers, workerId, owner, common.OwnerConsumerGroup, WorkerWaterline, metadata, w.Config.InstanceTTL)
	if err != nil || claimed == nil {
		return nil, err
	}
	return newWaterlineInstance(w, owner, claimed, parsed)
}
