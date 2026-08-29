package worker

import (
	"context"
	"errors"

	"github.com/agentstax/vulkan/pkg/common"
)

// A Definition is what one worker kind declares: the row's name, the config
// written onto it, who may own it, and how many instances may live at once.
// Declaring writes it to the worker_config table; the manager reads the row back
// and hands it to the kind's Provisioner.
type Definition struct {
	Name     string
	Metadata any

	OwnerKind       common.OwnerKind // common.OwnerAny lifts the declare guard
	TargetInstances int              // 0 if unset -> 1; NoInstanceTarget lifts the claim gate
}

func NewDefinition(name string, ownerKind common.OwnerKind, metadata any) (*Definition, error) {
	if name == "" {
		return nil, errors.New("name must not be empty")
	}
	if ownerKind != common.OwnerAny {
		if err := ownerKind.Validate(); err != nil {
			return nil, err
		}
	}
	if metadata == nil {
		return nil, errors.New("metadata must not be nil")
	}

	return &Definition{Name: name, OwnerKind: ownerKind, Metadata: metadata}, nil
}

// A Declarer states desired state: a worker row for the given owner.
// Declaring is repeatable -- an existing row takes the declaration's config,
// the newest wins -- so owners declare on every register and a declaration
// lost to a crash heals on the next one.
type Declarer interface {
	Declare(ctx context.Context, owner *common.Owner) error
}

// A Provisioner mints live copies of the rows its Definition names, each
// provisioned from the declared row the manager read back. A nil Execution
// means declined -- target_instances is already filled -- which is not an
// error; the manager retries next reconcile.
type Provisioner interface {
	Definition() *Definition
	Provision(ctx context.Context, declared *Worker) (Execution, error)
}

// An Execution is one provisioned life. Run blocks until the life ends, and
// its return value is the exit declaration:
//   - nil -> work complete
//   - ErrInstanceLost -> claim lost, a replacement may already be running -- respawnable
//   - anything else -> fatal for whoever spawned it
type Execution interface {
	Run(ctx context.Context) error
}
