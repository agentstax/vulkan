package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/agentstax/vulkan/pkg/common"
)

// Worker is one row of the worker table.
type Worker struct {
	Id              int64
	Name            string
	Owner           common.Owner
	Metadata        string // raw JSONB text; callers unmarshal their own shape
	TargetInstances int
}

func NewWorker(name string, owner *common.Owner, metadata json.RawMessage, targetInstances int) (*Worker, error) {
	if name == "" {
		return nil, errors.New("name is required")
	}
	if owner == nil {
		return nil, errors.New("owner must not be nil")
	}
	if targetInstances < 0 {
		return nil, fmt.Errorf("targetInstances must be >= 0, got %d", targetInstances)
	}
	if metadata == nil {
		metadata = json.RawMessage(`{}`)
	}
	return &Worker{
		Name:            name,
		Owner:           *owner,
		Metadata:        string(metadata),
		TargetInstances: targetInstances,
	}, nil
}

// ListWorkers lists every worker seeded in the worker table.
func (c *WorkerController) ListWorkers(ctx context.Context) ([]Worker, error) {
	listed, err := c.datastore.ListWorkers(ctx)
	if err != nil {
		return nil, err
	}

	var workers []Worker
	for _, data := range listed {
		// a bad row skips rather than erroring the whole list
		w, err := toWorker(data)
		if err != nil {
			c.Logger.WarnContext(ctx, "worker row owner unreadable -- skipping", "worker", data.Name, "error", err)
			continue
		}
		workers = append(workers, *w)
	}
	return workers, nil
}

// GetWorkerMetadata reads the (name, owner) row's metadata. Errors if the
// row was never seeded.
func (c *WorkerController) GetWorkerMetadata(ctx context.Context, name string, owner *common.Owner) (json.RawMessage, error) {
	return c.datastore.GetWorkerMetadata(ctx, name, owner)
}
