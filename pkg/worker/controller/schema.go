package controller

import (
	"context"
	"errors"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/migrate"
)

// AssertSchemaSupported gates a worker's Register: the schemas the owner
// depends on must sit within the range this build understands.
func (c *WorkerController) AssertSchemaSupported(ctx context.Context, owner *common.Owner) error {
	if owner == nil {
		return errors.New("owner must not be nil")
	}
	return migrate.AssertSchemaSupported(ctx, c.datastore.Datastore.Pool, owner)
}
