package controller

import (
	"errors"

	"github.com/agentstax/vulkan/pkg/common/logging"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	metricscontroller "github.com/agentstax/vulkan/pkg/metrics/controller"
)

type WorkerLivenessController struct {
	Logger logging.Logger

	metrics *metricscontroller.MetricsController
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewWorkerLivenessController(ds *iDatastore.PostgresDatastore, cfg *ControllerConfig) (*WorkerLivenessController, error) {
	if ds == nil {
		return nil, errors.New("datastore must not be nil")
	}
	if cfg == nil {
		cfg = &ControllerConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	metricsController, err := metricscontroller.NewMetricsController(ds, &metricscontroller.ControllerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	return &WorkerLivenessController{
		Logger:  cfg.Logger,
		metrics: metricsController,
	}, nil
}
