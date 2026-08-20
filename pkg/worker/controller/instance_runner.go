package controller

import (
	"context"
	"errors"
	"time"

	"github.com/agentstax/vulkan/pkg/common/logging"
	"github.com/agentstax/vulkan/pkg/worker"
)

// releaseWindow caps the instance release on shutdown.
const releaseWindow = 5 * time.Second

// InstanceRunner holds a claimed worker instance while work runs: a heartbeat
// renews the claim, a lost claim cancels the work's ctx with ErrInstanceLost,
// and the instance releases on the way out. Pacing is the caller's.
type InstanceRunner struct {
	Config *InstanceRunnerConfig
	Logger logging.Logger

	workers *WorkerController
	claimed *worker.WorkerInstance
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewInstanceRunner(workers *WorkerController, claimed *worker.WorkerInstance, cfg *InstanceRunnerConfig) (*InstanceRunner, error) {
	if workers == nil {
		return nil, errors.New("workers controller must not be nil")
	}
	if claimed == nil {
		return nil, errors.New("claimed instance must not be nil")
	}
	if cfg == nil {
		cfg = &InstanceRunnerConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return &InstanceRunner{
		Config:  cfg,
		Logger:  cfg.Logger,
		workers: workers,
		claimed: claimed,
	}, nil
}

// Run runs work while the heartbeat holds the claim; a requested stop returns
// nil. The claimed instance releases on the way out however Run exits.
// A lost claim returns ErrInstanceLost even when work only reports its ctx's
// cancellation -- work never inspects the cause itself.
func (r *InstanceRunner) Run(ctx context.Context, work func(context.Context) error) error {
	if work == nil {
		return errors.New("work must not be nil")
	}
	defer r.releaseInstance(ctx)

	workCtx, stopWork := context.WithCancelCause(ctx)
	heartbeatDone := r.startRenewalHeartbeat(workCtx, stopWork)

	err := work(workCtx)

	stopWork(nil)   // triggers heartbeat to drain
	<-heartbeatDone // wait for heartbeat to drain

	if cause := context.Cause(workCtx); errors.Is(cause, worker.ErrInstanceLost) {
		err = worker.ErrInstanceLost
	}

	switch {
	case errors.Is(err, worker.ErrInstanceLost):
		r.Logger.WarnContext(ctx, worker.EventInstanceLost.Message, "code", worker.EventInstanceLost.Code)
		return err
	case errors.Is(err, context.Canceled):
		return nil
	default:
		return err
	}
}

// startRenewalHeartbeat renews the claimed instance every InstanceTTL/2.
// ErrInstanceLost cancels the work: the row expired or was removed, a
// replacement may already be running. The returned channel closes when the
// heartbeat is fully stopped.
func (r *InstanceRunner) startRenewalHeartbeat(workCtx context.Context, stopWork context.CancelCauseFunc) <-chan struct{} {
	done := make(chan struct{})

	go func() {
		defer close(done)
		ticker := time.NewTicker(r.Config.InstanceTTL / 2)
		defer ticker.Stop()

		for {
			select {
			case <-workCtx.Done():
				return
			case <-ticker.C:
				err := r.workers.RenewInstance(workCtx, r.claimed.Id, r.claimed.Token, r.Config.InstanceTTL)
				if err == nil {
					continue
				}
				if errors.Is(err, worker.ErrInstanceLost) {
					stopWork(worker.ErrInstanceLost)
					return
				}
				if workCtx.Err() == nil {
					// keep working unrenewed -- worst case the row expires and
					// a replacement overlaps, which work must tolerate
					r.Logger.WarnContext(workCtx, "could not renew worker instance", "error", err)
				}
			}
		}
	}()
	return done
}

// releaseInstance runs however Run exits -- on shutdown the lifecycle ctx is
// already dead, so the release gets its own detached, capped ctx.
func (r *InstanceRunner) releaseInstance(ctx context.Context) {
	releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), releaseWindow)
	defer cancel()

	if err := r.workers.ReleaseInstance(releaseCtx, r.claimed.Id, r.claimed.Token); err != nil && !errors.Is(err, worker.ErrInstanceLost) {
		r.Logger.WarnContext(releaseCtx, "could not release worker instance -- a replacement waits out expires_at", "error", err)
	}
}
