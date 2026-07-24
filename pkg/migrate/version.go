package migrate

import (
	"context"

	"github.com/agentstax/vulkan/pkg/datastore"
	mDatastore "github.com/agentstax/vulkan/pkg/migrate/datastore"
)

// Version reads an entity's current schema version from schema_log,
// re-exported so callers depend only on pkg/migrate, not its datastore
// subpackage. Returns ErrNotRegistered if the entity has no baseline record.
func Version(ctx context.Context, q datastore.Querier, entityType string, entityID int64) (int64, error) {
	return mDatastore.Version(ctx, q, entityType, entityID)
}
