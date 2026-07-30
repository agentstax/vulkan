package migrate

import (
	"context"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/datastore"
	mDatastore "github.com/agentstax/vulkan/pkg/migrate/datastore"
)

// Version reads an owner's current schema version from migration_log,
// re-exported so callers depend only on pkg/migrate, not its datastore
// subpackage. Returns ErrNotRegistered if there is no baseline record.
func Version(ctx context.Context, q datastore.Querier, owner common.Owner) (int64, error) {
	return mDatastore.Version(ctx, q, owner)
}
