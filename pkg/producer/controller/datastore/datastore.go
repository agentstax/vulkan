package datastore

import (
	"errors"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/common/logging"
	"github.com/agentstax/vulkan/pkg/datastore"
)

type ProducerDatastore struct {
	Datastore      *datastore.PostgresDatastore
	DatastoreRetry *common.RetryDatastore // default Wrap classification covers everything except Commit -- classified inline at that call site
	Logger         logging.Logger

	createAheadGate    *createAheadGate
	createAheadTimeout time.Duration
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewProducerDatastore(ds *datastore.PostgresDatastore, cfg *ProducerDatastoreConfig) (*ProducerDatastore, error) {
	if ds == nil {
		return nil, errors.New("datastore must not be nil")
	}
	if cfg == nil {
		cfg = &ProducerDatastoreConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	datastoreRetry, err := common.NewRetryDatastore(cfg.Retry, cfg.Logger)
	if err != nil {
		return nil, err
	}

	// trigger create-ahead at 80% or 95% full partition
	createAheadGate, err := newCreateAheadGate([]float64{0.80, 0.95})
	if err != nil {
		return nil, err
	}

	// the full retry schedule plus per-attempt DB work -- so the timeout only
	// cuts what lock_timeout can't bound (head-read lock waits, network hangs)
	createAheadTimeout := datastoreRetry.CalculateTotalDelay() +
		time.Duration(datastoreRetry.MaxRetries)*createAheadAttemptAllowance

	return &ProducerDatastore{
		Datastore:          ds,
		DatastoreRetry:     datastoreRetry,
		Logger:             cfg.Logger,
		createAheadGate:    createAheadGate,
		createAheadTimeout: createAheadTimeout,
	}, nil
}
