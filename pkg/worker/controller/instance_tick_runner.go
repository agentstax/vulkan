package controller

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/worker"
)

// releaseWindow caps the instance release on shutdown.
const releaseWindow = 5 * time.Second

// InstanceTickRunner is the loop every claimed worker instance composes: Run
// paces the worker's pass at the row's poll_rate while a heartbeat holds the
// claim, records the success/failure streak, and releases the instance on
// the way out. Pass a logger.With-enriched Logger so the runner's lines
// carry the worker's identity.
type InstanceTickRunner struct {
	Config *InstanceTickRunnerConfig
	Logger logger.Logger // copied from Config.Logger at construction

	workers  *WorkerController
	claimed  *worker.WorkerInstance
	pollRate time.Duration
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewInstanceTickRunner(workers *WorkerController, claimed *worker.WorkerInstance, pollRate time.Duration, cfg *InstanceTickRunnerConfig) (*InstanceTickRunner, error) {
	if workers == nil {
		return nil, errors.New("workers controller must not be nil")
	}
	if claimed == nil {
		return nil, errors.New("claimed instance must not be nil")
	}
	if pollRate <= 0 {
		return nil, fmt.Errorf("pollRate must be > 0, got %v", pollRate)
	}
	if cfg == nil {
		cfg = &InstanceTickRunnerConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return &InstanceTickRunner{
		Config:   cfg,
		Logger:   cfg.Logger,
		workers:  workers,
		claimed:  claimed,
		pollRate: pollRate,
	}, nil
}

// Run paces onTick until ctx cancels or the claim is lost; a requested stop
// returns nil. The claimed instance releases on the way out however Run
// exits.
func (r *InstanceTickRunner) Run(ctx context.Context, onTick func(context.Context) error) error {
	if onTick == nil {
		return errors.New("pass must not be nil")
	}
	defer r.releaseInstance(ctx)

	workCtx, stopWork := context.WithCancelCause(ctx)
	heartbeatDone := r.startRenewalHeartbeat(workCtx, stopWork)

	err := r.tick(workCtx, onTick)

	stopWork(nil)   // triggers heartbeat to drain
	<-heartbeatDone // wait for heartbeat to drain

	switch {
	case errors.Is(err, worker.ErrInstanceLost):
		r.Logger.WarnContext(ctx, "worker instance lost -- stopping, a replacement may already be running")
		return err
	case errors.Is(err, context.Canceled):
		return nil
	default:
		return err
	}
}

// tick paces the onTick at the row's poll_rate. onTick errors are never fatal --
// a degraded worker doesn't take the process down -- so a failure logs,
// records the streak, and backs the next tick off.
func (r *InstanceTickRunner) tick(ctx context.Context, onTick func(context.Context) error) error {
	// rand first tick to avoid rollouts starting N replicas
	// at the same instant causing a request storm
	timer := time.NewTimer(time.Duration(rand.Float64() * float64(r.pollRate)))
	defer timer.Stop()

	attempts := r.claimed.Attempts
	for {
		select {
		case <-ctx.Done():
			if cause := context.Cause(ctx); errors.Is(cause, worker.ErrInstanceLost) {
				return worker.ErrInstanceLost
			}
			return ctx.Err()
		case <-timer.C:
		}

		err := onTick(ctx)

		// re-jittered every tick so replicas' phases keep drifting apart
		jitter := 1 + r.Config.JitterFraction*(2*rand.Float64()-1)
		delay := time.Duration(float64(r.pollRate) * jitter)

		switch {
		case err == nil:
			if attempts > 0 {
				attempts = 0
				if err := r.workers.RecordInstanceSuccess(ctx, r.claimed.Id, r.claimed.Token); err != nil {
					if errors.Is(err, worker.ErrInstanceLost) {
						return err
					}
					r.Logger.WarnContext(ctx, "worker success record failed", "error", err)
				}
			}
		case ctx.Err() != nil:
			// shutdown mid-pass -- the next select returns
		default:
			recorded, recordErr := r.workers.RecordInstanceFailure(ctx, r.claimed.Id, r.claimed.Token)
			switch {
			case recordErr == nil:
				attempts = recorded
			case errors.Is(recordErr, worker.ErrInstanceLost):
				return recordErr
			default:
				attempts++
				r.Logger.WarnContext(ctx, "worker failure record failed", "error", recordErr)
			}
			delay = max(delay, r.Config.TickRetry.CalculateDelay(attempts-1))
			r.Logger.ErrorContext(ctx, "worker tick failed -- backing off", "attempts", attempts, "delay", delay, "error", err)
		}

		timer.Reset(delay)
	}
}

// startRenewalHeartbeat renews the claimed instance every InstanceTTL/2.
// ErrInstanceLost cancels the work: the row expired or was removed, a
// replacement may already be running. The returned channel closes when the
// heartbeat is fully stopped.
func (r *InstanceTickRunner) startRenewalHeartbeat(workCtx context.Context, stopWork context.CancelCauseFunc) <-chan struct{} {
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
					// a replacement overlaps, which passes must tolerate
					r.Logger.WarnContext(workCtx, "worker instance renewal failed", "error", err)
				}
			}
		}
	}()
	return done
}

// releaseInstance runs however Run exits -- on shutdown the lifecycle ctx is
// already dead, so the release gets its own detached, capped ctx.
func (r *InstanceTickRunner) releaseInstance(ctx context.Context) {
	releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), releaseWindow)
	defer cancel()

	if err := r.workers.ReleaseInstance(releaseCtx, r.claimed.Id, r.claimed.Token); err != nil && !errors.Is(err, worker.ErrInstanceLost) {
		r.Logger.WarnContext(releaseCtx, "worker instance release failed -- a replacement waits out expires_at", "error", err)
	}
}
