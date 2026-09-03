package controller

import (
	"context"
	"errors"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/worker"
)

// DeclareWorker writes a definition as owner's worker row -- the newest
// declaration wins. Every kind's Declare ends here; the definition's
// OwnerKind is enforced against the owner, common.OwnerAny admits every kind.
func (c *WorkerController) DeclareWorker(ctx context.Context, definition *worker.Definition, owner *common.Owner) error {
	if definition == nil {
		return errors.New("definition must not be nil")
	}
	if owner == nil {
		return errors.New("owner must not be nil")
	}
	if definition.OwnerKind != common.OwnerAny {
		if err := ValidateOwner(owner, definition.OwnerKind, definition.Name); err != nil {
			return err
		}
	}

	return c.RegisterWorker(ctx, definition.Name, owner, &WorkerConfig{
		Metadata:        definition.Metadata,
		TargetInstances: definition.TargetInstances,
	})
}

// RegisterWorker creates the (name, owner) worker row, or writes cfg.Metadata
// onto the existing one -- the newest declaration wins. cfg.TargetInstances
// applies at creation only.
func (c *WorkerController) RegisterWorker(ctx context.Context, name string, owner *common.Owner, cfg *WorkerConfig) error {
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

	return c.datastore.RegisterWorker(ctx, name, owner, cfg.Metadata, int(cfg.TargetInstances), common.ProcessIdentity)
}

// ListWorkers lists the worker rows owned anywhere on owner's chain -- a
// sibling group's are not on it. A system owner's chain is everything.
func (c *WorkerController) ListWorkers(ctx context.Context, owner *common.Owner) ([]*worker.WorkerData, error) {
	if owner == nil {
		return nil, errors.New("owner must not be nil")
	}
	listed, err := c.datastore.ListWorkers(ctx, owner)
	if err != nil {
		return nil, err
	}

	var workers []*worker.WorkerData
	for _, data := range listed {
		// a bad row skips rather than erroring the whole list
		listedWorker, err := toWorkerData(data)
		if err != nil {
			c.Logger.WarnContext(ctx, "could not read worker row owner -- skipping", "worker", data.Name, "error", err)
			continue
		}
		workers = append(workers, listedWorker)
	}
	return workers, nil
}

// GetWorker reads the (name, owner) worker row. Errors if the row was never
// declared.
func (c *WorkerController) GetWorker(ctx context.Context, name string, owner *common.Owner) (*worker.WorkerData, error) {
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
