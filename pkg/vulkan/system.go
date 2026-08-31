package vulkan

import (
	"context"
	"errors"

	"github.com/agentstax/vulkan/pkg/migrate"
)

// System is a handle on the singleton system, holding no row. Get is the
// comma-ok read; every other verb returns the not-registered error itself.
type System struct {
	client *Client
}

// System names the system on the client. No I/O and no failure -- each
// verb on the handle resolves the system when called.
func (c *Client) System() *System {
	return &System{client: c}
}

// Get reads the system's row. Returns (nil, nil) when no system is
// registered.
func (s *System) Get(ctx context.Context) (*SystemData, error) {
	sys, err := s.client.admin.GetSystem(ctx)
	if errors.Is(err, migrate.ErrNotRegistered) {
		return nil, nil
	}
	return sys, err
}

// Migrate moves the system schema to targetVersion.
func (s *System) Migrate(ctx context.Context, targetVersion int64) error {
	return s.client.admin.MigrateSystem(ctx, targetVersion)
}

// Destroy permanently deletes every topic, schedule, consumer group,
// worker, and the shared control-plane tables. Refused unless
// ClientConfig.AllowDestroy is set.
func (s *System) Destroy(ctx context.Context, options *DestroyOptions) error {
	return s.client.admin.DestroySystem(ctx, options)
}
