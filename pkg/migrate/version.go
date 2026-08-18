package migrate

import (
	"context"
	"fmt"

	"github.com/agentstax/vulkan/pkg/common"
)

// SystemVersion reads the system's current schema version from migration_log.
// Returns ErrNotRegistered if there is no baseline record.
func (c *Controller) SystemVersion(ctx context.Context, systemId int64) (int64, error) {
	if systemId <= 0 {
		return 0, fmt.Errorf("systemId must be > 0, got %d", systemId)
	}
	return c.datastore.SystemVersion(ctx, systemId)
}

// TopicVersion reads a topic's current schema version from migration_log.
// Returns ErrNotRegistered if there is no baseline record.
func (c *Controller) TopicVersion(ctx context.Context, topicId int64) (int64, error) {
	if topicId <= 0 {
		return 0, fmt.Errorf("topicId must be > 0, got %d", topicId)
	}
	return c.datastore.TopicVersion(ctx, topicId)
}

// SystemOwner resolves the singleton system row to its owner.
// Returns ErrNotRegistered if RegisterSystem hasn't run.
func (c *Controller) SystemOwner(ctx context.Context) (*common.Owner, error) {
	return c.datastore.SystemOwner(ctx)
}
