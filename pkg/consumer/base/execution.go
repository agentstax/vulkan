package base

import (
	"context"
	"errors"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/concurrency"
	"github.com/agentstax/vulkan/pkg/worker"
	workercontroller "github.com/agentstax/vulkan/pkg/worker/controller"
)

// BaseExecution runs a row's loop while the instance's heartbeat holds
// the claim; the claimed instance releases on the way out however Run exits.
type BaseExecution struct {
	instanceRunner *workercontroller.InstanceRunner
	run            func(ctx context.Context) error
	permit         *concurrency.Permit // never released -- Run is one-shot
}

func NewBaseExecution[Message any](baseDefinition *BaseDefinition[Message], owner *common.Owner, claimed *worker.WorkerInstance, instanceTTL time.Duration, run func(ctx context.Context) error) (*BaseExecution, error) {
	if baseDefinition == nil {
		return nil, errors.New("definition base must not be nil")
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

	instanceRunner, err := workercontroller.NewInstanceRunner(baseDefinition.workers, claimed, &workercontroller.InstanceRunnerConfig{
		InstanceTTL: instanceTTL,
		Logger:      common.LoggerWith(baseDefinition.Logger, "worker", baseDefinition.workerName, "owner", owner.Name),
	})
	if err != nil {
		return nil, err
	}

	permit, err := concurrency.NewPermit()
	if err != nil {
		return nil, err
	}

	return &BaseExecution{
		instanceRunner: instanceRunner,
		run:            run,
		permit:         permit,
	}, nil
}

// a BaseExecution wraps one claimed worker instance -- once Run returns that
// instance is released, so the execution is spent and can never run again.
func (e *BaseExecution) Run(ctx context.Context) error {
	if _, ok := e.permit.Acquire(); !ok {
		return errors.New("execution already ran -- its claimed worker instance is released")
	}

	return e.instanceRunner.Run(ctx, e.run)
}
