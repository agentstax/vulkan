package manager

import (
	"context"
	"errors"
	"math/rand/v2"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/worker"
)

// Runner keeps a claimed manager execution running for an owner across lives
// -- the self-heal a manager gives the workers it spawns, given to the manager
// itself. Safe only for manager rows: a target-gated worker re-claiming itself
// would take back an instance another claim just won.
type Runner struct {
	Owner  *common.Owner
	Config *RunnerConfig
	Logger logger.Logger

	definition *ManagerDefinition
}

func NewRunner(definition *ManagerDefinition, owner *common.Owner, cfg *RunnerConfig) (*Runner, error) {
	if definition == nil {
		return nil, errors.New("definition must not be nil")
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
		Owner:      owner,
		Config:     cfg,
		Logger:     cfg.Logger,
		definition: definition,
	}, nil
}

// Run claims and runs manager executions until ctx cancels; a requested stop
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
			r.Logger.WarnContext(ctx, "manager re-claim failed -- retrying", "owner", r.Owner.Name, "error", err)
		case execution == nil:
			r.Logger.WarnContext(ctx, "manager row suspended -- its chain goes unreconciled until target_instances is restored", "owner", r.Owner.Name)
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

// claim re-reads the row every life, so a metadata edit lands on the next
// one. nil = suspended.
func (r *Runner) claim(ctx context.Context) (worker.Execution, error) {
	row, err := r.definition.workers.GetWorker(ctx, WorkerManager, r.Owner)
	if err != nil {
		return nil, err
	}
	return r.definition.Provision(ctx, row.Id, &row.Owner, row.Metadata)
}
