package worker

import (
	"context"

	"github.com/agentstax/vulkan/pkg/common"
)

// A Seeder creates a worker row for the given owner. Seeding is idempotent
// -- an existing row is left untouched -- so owners run their seeders on
// every register and a seed lost to a crash heals on the next one.
type Seeder interface {
	Seed(ctx context.Context, owner *common.Owner) error
}

// A Factory is a non-registered worker: Name says which worker rows it
// runs, Register claims one live instance for a row and returns it. A nil
// Instance means declined -- target_instances is already filled -- which is
// not an error; the manager retries next reconcile.
type Factory interface {
	Name() string
	Register(ctx context.Context, workerId int64, owner *common.Owner, metadata any) (Instance, error)
}

// Instance is what Register returns: one claimed life. Run blocks until the
// life ends, and its return value is the instance's exit declaration:
//   - nil -> work complete
//   - ErrInstanceLost -> claim lost, a replacement may already be running -- respawnable
//   - anything else -> fatal for whoever spawned it
type Instance interface {
	Run(ctx context.Context) error
}
