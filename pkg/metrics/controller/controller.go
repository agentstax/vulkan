package controller

import (
	"errors"

	coredatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/metrics/controller/datastore"
)

// MetricsController is the single read surface for the DB-snapshot metrics.
type MetricsController struct {
	Logger logger.Logger

	datastore *datastore.MetricsDatastore
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewMetricsController(ds *coredatastore.PostgresDatastore, cfg *ControllerConfig) (*MetricsController, error) {
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

	metricsDatastore, err := datastore.NewMetricsDatastore(ds, &datastore.MetricsDatastoreConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	return &MetricsController{
		Logger:    cfg.Logger,
		datastore: metricsDatastore,
	}, nil
}
