package maintain

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/agentstax/vulkan/pkg/logger"
)

// releaseWindow caps the release of a duty on shutdown.
const releaseWindow = 5 * time.Second

// hungWorkFactor is number of heartbeats before duty work
// is considered 'hung'
const hungWorkFactor = 10

// dutyRunner is the duty/claim machinery every duty composes:
// - jittered ticks
// - claim race
// - renewal heartbeat
// - keep-or-release
// The owning duty constructs it at Register and hands run its work.
type dutyRunner struct {
	ds     *MaintenanceDatastore
	logger logger.Logger
	jitter float64

	kind    string // DutyJanitor | DutyWaterline
	topicID int64
	group   string // "" for topic-scoped duties
	rate    time.Duration
}

func newDutyRunner(ds *MaintenanceDatastore, log logger.Logger, jitter float64, kind string, topicID int64, group string, rate time.Duration) (*dutyRunner, error) {
	if ds == nil {
		return nil, errors.New("datastore must not be nil")
	}
	if log == nil {
		return nil, errors.New("logger must not be nil")
	}
	if kind != DutyJanitor && kind != DutyWaterline {
		return nil, fmt.Errorf("unknown duty kind %q", kind)
	}
	if topicID <= 0 {
		return nil, fmt.Errorf("topicID must be > 0, got %d", topicID)
	}
	if rate <= 0 {
		return nil, fmt.Errorf("rate must be > 0, got %v", rate)
	}
	return &dutyRunner{
		ds:      ds,
		logger:  log,
		jitter:  jitter,
		kind:    kind,
		topicID: topicID,
		group:   group,
		rate:    rate,
	}, nil
}

// run ticks until ctx cancels.
func (d *dutyRunner) run(ctx context.Context, work func(context.Context) error) error {
	// rand first tick to avoid rollouts starting N replicas
	// at the same instant causing a request storm
	timer := time.NewTimer(time.Duration(rand.Float64() * float64(d.rate)))
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}

		d.tick(ctx, work)

		// re-jittered every tick so replicas' phases keep drifting apart
		jitter := 1 + d.jitter*(2*rand.Float64()-1)
		timer.Reset(time.Duration(float64(d.rate) * jitter))
	}
}

// tick claims -> works -> keeps or releases the duty claim.
// Duty errors should never be fatal. If maintenance degrades,
// it doesn't take the process down -- so every outcome
// logs and waits for the next interval.
func (d *dutyRunner) tick(ctx context.Context, work func(context.Context) error) {
	claim, err := d.ds.ClaimDuty(ctx, d.kind, d.topicID, d.group, d.rate)
	if err != nil {
		if ctx.Err() == nil {
			d.logger.ErrorContext(ctx, "duty claim failed", "duty", d.kind, "topic", d.topicID, "group", d.group, "error", err)
		}
		return
	}
	if claim == nil {
		return // another maintainer's turn
	}

	workCtx, stopWork := context.WithCancelCause(ctx)
	heartbeatDone := d.startRenewalHeartbeat(workCtx, stopWork, claim)

	err = work(workCtx)

	stopWork(nil)   // triggers heartbeat to drain
	<-heartbeatDone // wait for heartbeat to drain

	if err == nil {
		if err := d.ds.ResetDuty(ctx, claim); err != nil && !errors.Is(err, ErrDutyLost) {
			d.logger.WarnContext(ctx, "duty reset failed", "duty", d.kind, "topic", d.topicID, "group", d.group, "error", err)
		}
		return // success keeps the claim -- the claim IS the schedule
	}

	switch {
	case ctx.Err() != nil:
		// shutdown mid-duty -- release on a detached ctx (this one is dead).
		// min(d.rate, releaseWindow) b/c a release slower than one tick is
		// worthless -- the claim is already expired and nothing needs released.
		releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), min(d.rate, releaseWindow))
		defer cancel()
		if err := d.ds.ReleaseDuty(releaseCtx, claim); err != nil && !errors.Is(err, ErrDutyLost) {
			d.logger.WarnContext(releaseCtx, "duty release failed on shutdown -- next run waits out the interval", "duty", d.kind, "topic", d.topicID, "group", d.group, "error", err)
		}
	case errors.Is(err, ErrDutyLost) || errors.Is(context.Cause(workCtx), ErrDutyLost):
		d.logger.InfoContext(ctx, "duty ceded mid-run to another maintainer", "duty", d.kind, "topic", d.topicID, "group", d.group)
	default:
		delay, backoffErr := d.ds.BackoffDuty(ctx, claim)
		if backoffErr != nil && !errors.Is(backoffErr, ErrDutyLost) {
			d.logger.WarnContext(ctx, "duty backoff write failed", "duty", d.kind, "topic", d.topicID, "group", d.group, "error", backoffErr)
		}
		d.logger.ErrorContext(ctx, "duty run failed -- backing off", "duty", d.kind, "topic", d.topicID, "group", d.group, "attempts", claim.Attempts, "delay", delay, "error", err)
	}
}

// startRenewalHeartbeat renews the claim every rate/2 while the duty works.
// ErrDutyLost cancels the work: the fence tripped, another maintainer owns
// the duty. The returned channel closes when the heartbeat is fully stopped.
func (d *dutyRunner) startRenewalHeartbeat(workCtx context.Context, stopWork context.CancelCauseFunc, claim *DutyClaim) <-chan struct{} {
	done := make(chan struct{})

	go func() {
		defer close(done)
		ticker := time.NewTicker(d.rate / 2)
		defer ticker.Stop()

		ticks := 0
		for {
			select {
			case <-workCtx.Done():
				return
			case <-ticker.C:
				// track if work is taking too long ie 'hung'
				ticks++
				if ticks%(2*hungWorkFactor) == 0 {
					d.logger.WarnContext(workCtx, "duty work still running -- possibly hung", "duty", d.kind, "topic", d.topicID, "group", d.group, "running_for", time.Duration(ticks)*d.rate/2)
				}

				err := d.ds.RenewDuty(workCtx, claim, d.rate)
				if err == nil {
					continue
				}
				if errors.Is(err, ErrDutyLost) {
					stopWork(ErrDutyLost)
					return
				}
				if workCtx.Err() == nil {
					// keep working unrenewed -- worst case the claim lapses and
					// another maintainer overlaps, which the ops tolerate
					d.logger.WarnContext(workCtx, "duty renewal failed", "duty", d.kind, "topic", d.topicID, "group", d.group, "error", err)
				}
			}
		}
	}()
	return done
}
