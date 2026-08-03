package controller

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/worker"
)

// InsertWorker creates the (name, owner) worker row; an existing row is left
// untouched. cfg may be nil or a sparse struct -- WithDefaults fills every
// field left unset, Validate rejects what's out of range.
func (c *WorkerController) InsertWorker(ctx context.Context, name string, owner *common.Owner, cfg *WorkerConfig) error {
	if name == "" {
		return errors.New("name is required")
	}
	if owner == nil {
		return errors.New("owner must not be nil")
	}
	if cfg == nil {
		cfg = &WorkerConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return err
	}

	return c.datastore.InsertWorker(ctx, name, owner, cfg.Metadata, cfg.TargetInstances)
}

// ListWorkers lists every worker seeded in the worker table.
func (c *WorkerController) ListWorkers(ctx context.Context) ([]*worker.Worker, error) {
	listed, err := c.datastore.ListWorkers(ctx)
	if err != nil {
		return nil, err
	}

	var workers []*worker.Worker
	for _, data := range listed {
		// a bad row skips rather than erroring the whole list
		listedWorker, err := toWorker(data)
		if err != nil {
			c.Logger.WarnContext(ctx, "worker row owner unreadable -- skipping", "worker", data.Name, "error", err)
			continue
		}
		workers = append(workers, listedWorker)
	}
	return workers, nil
}

// GetWorkerMetadata reads the (name, owner) row's metadata. Errors if the
// row was never seeded.
func (c *WorkerController) GetWorkerMetadata(ctx context.Context, name string, owner *common.Owner) (json.RawMessage, error) {
	if name == "" {
		return nil, errors.New("name is required")
	}
	if owner == nil {
		return nil, errors.New("owner must not be nil")
	}
	return c.datastore.GetWorkerMetadata(ctx, name, owner)
}
