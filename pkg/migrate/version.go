package migrate

import (
	"context"
	"errors"

	"github.com/agentstax/vulkan/pkg/common"
)

// Version reads an owner's current schema version from migration_log.
// Returns ErrNotRegistered if there is no baseline record.
func (c *Controller) Version(ctx context.Context, owner *common.Owner) (int64, error) {
	if owner == nil {
		return 0, errors.New("owner must not be nil")
	}
	return c.datastore.Version(ctx, owner)
}

// SystemOwner resolves the singleton system row to its owner.
// Returns ErrNotRegistered if RegisterSystem hasn't run.
func (c *Controller) SystemOwner(ctx context.Context) (*common.Owner, error) {
	return c.datastore.SystemOwner(ctx)
}
