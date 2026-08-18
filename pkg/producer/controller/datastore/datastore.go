package datastore

import (
	"errors"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/datastore"
)

type ProducerDatastore[Message any] struct {
	Datastore      *datastore.PostgresDatastore
	DatastoreRetry *common.RetryDatastore // default Wrap classification covers everything except Commit -- classified inline at that call site
	Logger         common.Logger

	createAheadGate    *CreateAheadGate
	createAheadTimeout time.Duration
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewProducerDatastore[Message any](ds *datastore.PostgresDatastore, cfg *ProducerDatastoreConfig) (*ProducerDatastore[Message], error) {
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
	createAheadGate, err := NewCreateAheadGate([]float64{0.80, 0.95})
	if err != nil {
		return nil, err
	}

	// the full retry schedule plus per-attempt DB work -- so the timeout only
	// cuts what lock_timeout can't bound (head-read lock waits, network hangs)
	createAheadTimeout := datastoreRetry.CalculateTotalDelay() +
		time.Duration(datastoreRetry.MaxRetries)*createAheadAttemptAllowance

	return &ProducerDatastore[Message]{
		Datastore:          ds,
		DatastoreRetry:     datastoreRetry,
		Logger:             cfg.Logger,
		createAheadGate:    createAheadGate,
		createAheadTimeout: createAheadTimeout,
	}, nil
}
