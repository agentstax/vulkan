package controller

import (
	"errors"
	"fmt"

	coredatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/topic/controller/datastore"
	"github.com/agentstax/vulkan/pkg/worker"
)

type TopicController struct {
	Logger logger.Logger

	declarers []worker.Declarer
	datastore *datastore.TopicDatastore
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range. declarers run on every
// RegisterTopic to create the registered topic's worker rows -- pass them
// only from a registrar; a controller built for reads needs none.
func NewTopicController(ds *coredatastore.PostgresDatastore, cfg *ControllerConfig, declarers ...worker.Declarer) (*TopicController, error) {
	if ds == nil {
		return nil, errors.New("datastore must not be nil")
	}
	for i, declarer := range declarers {
		if declarer == nil {
			return nil, fmt.Errorf("declarer %d must not be nil", i)
		}
	}
	if cfg == nil {
		cfg = &ControllerConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	topicDatastore, err := datastore.NewTopicDatastore(ds, &datastore.TopicDatastoreConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	return &TopicController{
		Logger:    cfg.Logger,
		declarers: declarers,
		datastore: topicDatastore,
	}, nil
}
