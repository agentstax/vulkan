package datastore

import (
	"os"

	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/retry"
)

type MigrateDatastore struct {
	Datastore *datastore.PostgresDatastore
	Retry     *retry.DatastoreRetry
	Logger    logger.Logger
}

func NewMigrateDatastore(ds *datastore.PostgresDatastore, retryPolicy *retry.Policy, log logger.Logger) (*MigrateDatastore, error) {
	if log == nil {
		log = logger.NewDefaultLogger(os.Stdout)
	}

	dsRetry, err := retry.NewDatastoreRetry(retryPolicy, log)
	if err != nil {
		return nil, err
	}

	return &MigrateDatastore{
		Datastore: ds,
		Retry:     dsRetry,
		Logger:    log,
	}, nil
}
