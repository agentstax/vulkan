package controller

import (
	"context"
	"errors"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/worker"
)

// InsertWorker creates the (name, owner) worker row.
// Metadata is merged between incoming values and existing overrides.
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

// ListWorkers lists the worker rows owned anywhere on owner's chain -- a
// sibling group's are not on it. A system owner's chain is everything.
func (c *WorkerController) ListWorkers(ctx context.Context, owner *common.Owner) ([]*worker.Worker, error) {
	if owner == nil {
		return nil, errors.New("owner must not be nil")
	}
	listed, err := c.datastore.ListWorkers(ctx, owner)
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

// GetWorker reads the (name, owner) worker row. Errors if the row was never
// declared.
func (c *WorkerController) GetWorker(ctx context.Context, name string, owner *common.Owner) (*worker.Worker, error) {
	if name == "" {
		return nil, errors.New("name is required")
	}
	if owner == nil {
		return nil, errors.New("owner must not be nil")
	}
	data, err := c.datastore.GetWorker(ctx, name, owner)
	if err != nil {
		return nil, err
	}
	return toOwnedWorker(data, owner), nil
}
