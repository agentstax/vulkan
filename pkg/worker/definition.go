package worker

import (
	"context"

	"github.com/agentstax/vulkan/pkg/common"
)

// A Declarer states desired state: a worker row for the given owner.
// Declaring is repeatable -- an existing row takes the declaration's config,
// the newest wins -- so owners declare on every register and a declaration
// lost to a crash heals on the next one.
type Declarer interface {
	Declare(ctx context.Context, owner *common.Owner) error
}

// A Provisioner mints live copies of the worker rows Name matches. A nil
// Execution means declined -- target_instances is already filled -- which is
// not an error; the manager retries next reconcile.
type Provisioner interface {
	Name() string
	Provision(ctx context.Context, workerId int64, owner *common.Owner, metadata any) (Execution, error)
}

// A Definition is one worker kind's whole lifecycle: it declares the row it
// runs and provisions executions of it.
type Definition interface {
	Declarer
	Provisioner
}

// An Execution is one provisioned life. Run blocks until the life ends, and
// its return value is the exit declaration:
//   - nil -> work complete
//   - ErrInstanceLost -> claim lost, a replacement may already be running -- respawnable
//   - anything else -> fatal for whoever spawned it
type Execution interface {
	Run(ctx context.Context) error
}
