package base

import (
	"context"
	"errors"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/common/logging"
	"github.com/agentstax/vulkan/pkg/concurrency"
	"github.com/agentstax/vulkan/pkg/worker"
	workercontroller "github.com/agentstax/vulkan/pkg/worker/controller"
)

// BaseInstance runs a row's loop while the instance's heartbeat holds
// the claim; the claimed instance releases on the way out however Run exits.
type BaseInstance struct {
	instanceRunner *workercontroller.InstanceRunner
	run            func(ctx context.Context) error
	permit         *concurrency.Permit // never released -- Run is one-shot
}

func NewBaseInstance[Message any](baseProvisioner *BaseProvisioner[Message], owner *common.Owner, claimed *worker.WorkerInstance, instanceTTL time.Duration, run func(ctx context.Context) error) (*BaseInstance, error) {
	if baseProvisioner == nil {
		return nil, errors.New("provisioner base must not be nil")
	}
	if owner == nil {
		return nil, errors.New("owner must not be nil")
	}
	if claimed == nil {
		return nil, errors.New("claimed worker instance must not be nil")
	}
	if run == nil {
		return nil, errors.New("run must not be nil")
	}

	instanceRunner, err := workercontroller.NewInstanceRunner(baseProvisioner.workers, claimed, &workercontroller.InstanceRunnerConfig{
		InstanceTTL: instanceTTL,
		Logger:      logging.LoggerWith(baseProvisioner.Logger, "worker", baseProvisioner.definition.Name, "owner", owner.Name),
	})
	if err != nil {
		return nil, err
	}

	permit, err := concurrency.NewPermit()
	if err != nil {
		return nil, err
	}

	return &BaseInstance{
		instanceRunner: instanceRunner,
		run:            run,
		permit:         permit,
	}, nil
}

// a BaseInstance wraps one claimed worker instance -- once Run returns that
// instance is released, so a BaseInstance never runs twice.
func (i *BaseInstance) Run(ctx context.Context) error {
	if _, ok := i.permit.Acquire(); !ok {
		return errors.New("Run already completed -- the claimed worker instance is released")
	}

	return i.instanceRunner.Run(ctx, i.run)
}
