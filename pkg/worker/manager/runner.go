package manager

import (
	"context"
	"errors"
	"math/rand/v2"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/common/logging"
	"github.com/agentstax/vulkan/pkg/worker"
)

// Runner keeps a claimed manager instance running for an owner across lives
// -- the self-heal a manager gives the workers it spawns, given to the manager
// itself, since no manager spawns the manager. A declined claim is not a
// failure: the row may be suspended, or its instances already at target in
// another process, so the loop waits out RetryDelay and tries again.
type Runner struct {
	Owner  *common.Owner
	Config *RunnerConfig
	Logger logging.Logger

	provisioner *ManagerProvisioner
}

func NewRunner(provisioner *ManagerProvisioner, owner *common.Owner, cfg *RunnerConfig) (*Runner, error) {
	if provisioner == nil {
		return nil, errors.New("provisioner must not be nil")
	}
	if owner == nil {
		return nil, errors.New("owner must not be nil")
	}
	if cfg == nil {
		cfg = &RunnerConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return &Runner{
		Owner:       owner,
		Config:      cfg,
		Logger:      cfg.Logger,
		provisioner: provisioner,
	}, nil
}

// Run claims and runs manager instances until ctx cancels; a requested stop
// returns nil.
// error before the first claim -> return it, the row is misconfigured
// error after -> log and retry, degraded upkeep never stops the host process
func (r *Runner) Run(ctx context.Context) error {
	claimed := false
	for {
		execution, err := r.claim(ctx)
		switch {
		case err != nil:
			if ctx.Err() != nil {
				return nil
			}
			if !claimed {
				return err
			}
			r.Logger.WarnContext(ctx, "could not re-claim manager row -- retrying", "owner", r.Owner.Name, "error", err)
		// nothing to run this life -- claim logged which case it was
		case execution == nil:
		default:
			claimed = true
			if err := execution.Run(ctx); !errors.Is(err, worker.ErrInstanceLost) {
				return err // nil on a requested stop
			}
		}

		// re-jittered every retry -- replicas that lost their claims together
		// must not re-claim in step
		jitter := 1 + r.Config.JitterFraction*(2*rand.Float64()-1)
		timer := time.NewTimer(time.Duration(float64(r.Config.RetryDelay) * jitter))
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}

// claim re-reads the row every life, so an edit to its metadata or
// target_instances lands on the next one. nil = nothing to run this life.
func (r *Runner) claim(ctx context.Context) (worker.Execution, error) {
	row, err := r.provisioner.workers.GetWorker(ctx, WorkerManager, r.Owner)
	if err != nil {
		return nil, err
	}

	// target_instances 0 declines every claim, so the operator hears why
	// instead of the attempt being made
	if row.TargetInstances == 0 {
		r.Logger.WarnContext(ctx, worker.EventManagerRowSuspended.Message, "code", worker.EventManagerRowSuspended.Code, "owner", r.Owner.Name)
		return nil, nil
	}

	execution, err := r.provisioner.Provision(ctx, row)
	if err != nil {
		return nil, err
	}
	if execution == nil {
		r.Logger.DebugContext(ctx, "manager row declined an instance -- retrying the claim", "owner", r.Owner.Name, "target_instances", row.TargetInstances)
	}
	return execution, nil
}
