package migrate

import (
	"context"

	"github.com/agentstax/vulkan/pkg/datastore"
	mDatastore "github.com/agentstax/vulkan/pkg/migrate/datastore"
)

// IsLocked reports whether any session currently holds the migration advisory
// lock, re-exported so callers depend only on pkg/migrate, not its datastore
// subpackage. A snapshot, not a guarantee -- see mDatastore.IsLocked.
func IsLocked(ctx context.Context, q datastore.Querier) (bool, error) {
	return mDatastore.IsLocked(ctx, q)
}
