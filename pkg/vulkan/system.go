package vulkan

import (
	"context"
	"errors"

	"github.com/agentstax/vulkan/pkg/migrate"
)

// SystemHandle is a handle on the singleton system, holding no row. Get is the
// comma-ok read; every other verb returns the not-registered error itself.
type SystemHandle struct {
	client *Client
}

// System names the system on the client. No I/O and no failure -- each
// verb on the handle resolves the system when called.
func (c *Client) System() *SystemHandle {
	return &SystemHandle{client: c}
}

// Register declares the system's own knobs and built-in alert schedules.
// Safe to run on every startup; cfg may be nil.
func (s *SystemHandle) Register(ctx context.Context, cfg *RegisterSystemConfig) error {
	return s.client.admin.RegisterSystem(ctx, cfg)
}

// Get reads the system's row. Returns (nil, nil) when no system is
// registered.
func (s *SystemHandle) Get(ctx context.Context) (*System, error) {
	sys, err := s.client.admin.GetSystem(ctx)
	if errors.Is(err, migrate.ErrNotRegistered) {
		return nil, nil
	}
	return sys, err
}

// Migrate moves the system's tables to targetVersion.
func (s *SystemHandle) Migrate(ctx context.Context, targetVersion int64) error {
	return s.client.admin.MigrateSystem(ctx, targetVersion)
}

// MigrateTopics moves every registered topic to targetVersion.
func (s *SystemHandle) MigrateTopics(ctx context.Context, targetVersion int64) error {
	return s.client.admin.MigrateTopics(ctx, targetVersion)
}

// Bindings returns every group's effective binding declaration
// and any declarers still waiting to change it, ordered by topic then
// group. A group that never declared a set reads the whole topic and does
// not appear.
func (s *SystemHandle) Bindings(ctx context.Context) ([]*Binding, error) {
	return s.client.admin.ListBindings(ctx)
}

// Destroy permanently deletes every topic, schedule, consumer group,
// worker, and the shared control-plane tables. Refused unless
// ClientConfig.AllowDestroy is set.
func (s *SystemHandle) Destroy(ctx context.Context, options *DestroyOptions) error {
	return s.client.admin.DestroySystem(ctx, options)
}
