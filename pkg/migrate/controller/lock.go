package controller

import (
	"context"
)

// IsLocked reports whether any session currently holds the migration advisory
// lock. A snapshot, not a guarantee -- see the datastore's IsLocked.
func (c *Controller) IsLocked(ctx context.Context) (bool, error) {
	return c.datastore.IsLocked(ctx)
}
