package controller

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/agentstax/vulkan/pkg/common/logging"
	"github.com/agentstax/vulkan/pkg/worker"
)

// paces a worker's pass at the row's poll_rate while an InstanceRunner holds
// the claim, recording the success/failure streak. Every tick-paced execution
// composes one. Pass a logging.LoggerWith-enriched Logger so its lines carry the
// worker's identity.
type InstanceTickRunner struct {
	Config *InstanceTickRunnerConfig
	Logger logging.Logger

	runner   *InstanceRunner
	workers  *WorkerController
	claimed  *worker.WorkerInstance
	pollRate time.Duration
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewInstanceTickRunner(workers *WorkerController, claimed *worker.WorkerInstance, pollRate time.Duration, cfg *InstanceTickRunnerConfig) (*InstanceTickRunner, error) {
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

	runner, err := NewInstanceRunner(workers, claimed, &InstanceRunnerConfig{
		InstanceTTL: cfg.InstanceTTL,
		Logger:      cfg.Logger,
	})
	if err != nil {
		return nil, err
	}

	return &InstanceTickRunner{
		Config:   cfg,
		Logger:   cfg.Logger,
		runner:   runner,
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
		return errors.New("onTick must not be nil")
	}
	return r.runner.Run(ctx, func(workCtx context.Context) error {
		return r.ticker(workCtx, onTick)
	})
}

// ticker paces onTick at the row's poll_rate. onTick errors are never fatal --
// a degraded worker doesn't take the process down -- so a failure logs,
// records the streak, and backs the next tick off.
func (r *InstanceTickRunner) ticker(ctx context.Context, onTick func(context.Context) error) error {
	// first tick is immediate -- claim arbitration already caps a row at
	// target_instances tickers, so a staggered start spreads nothing
	timer := time.NewTimer(0)
	defer timer.Stop()

	attempts := r.claimed.Attempts
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}

		tickStart := time.Now()
		err := onTick(logging.WithLogBuffer(ctx))
		if duration := time.Since(tickStart); duration > r.pollRate {
			r.Logger.WarnContext(ctx, worker.EventSlowTick.Message, "code", worker.EventSlowTick.Code, "duration", duration, "rate", r.pollRate)
		}

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
					r.Logger.WarnContext(ctx, "could not record worker success", "error", err)
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
				r.Logger.WarnContext(ctx, "could not record worker failure", "error", recordErr)
			}
			delay = max(delay, r.Config.TickRetry.CalculateDelay(attempts-1))

			// a streak past the curve's cap stopped being a blip -- escalate
			if attempts > r.Config.TickRetry.MaxRetries {
				r.Logger.ErrorContext(ctx, worker.EventTickBackoffCurveExhausted.Message, "code", worker.EventTickBackoffCurveExhausted.Code, "attempts", attempts, "delay", delay, "error", err)
			} else {
				r.Logger.WarnContext(ctx, "could not run worker tick -- backing off", "attempts", attempts, "delay", delay, "error", err)
			}
		}

		timer.Reset(delay)
	}
}
