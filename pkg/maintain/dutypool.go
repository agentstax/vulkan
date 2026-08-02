package maintain

import (
	"context"
	"sync"

	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/logger"
)

// dutyPool supervises the goroutine behind every duty the fleet has spawned:
type dutyPool struct {
	logger   logger.Logger
	ds       *datastore.PostgresDatastore
	config   *MaintainerConfig // fed to every constructed duty
	duties   []DutyConstructor // set by the fleet's Register
	running  map[FleetDuty]*spawnedDuty
	inflight sync.WaitGroup // every spawned Run goroutine, including stopped ones still draining
}

type spawnedDuty struct {
	stop context.CancelFunc
	done chan struct{} // closed when the duty's Run returns
}

// changeType classifies one duty key's difference between the desired set
// and the current running set.
type changeType int

const (
	dutyAdded   changeType = iota // desired but not running
	dutyRemoved                   // running but no longer desired
	dutyExited                    // running, but its Run goroutine already returned
)

type dutyChange struct {
	change changeType
	key    FleetDuty
}

func newDutyPool(log logger.Logger, ds *datastore.PostgresDatastore, cfg *MaintainerConfig) *dutyPool {
	return &dutyPool{
		logger:  log,
		ds:      ds,
		config:  cfg,
		running: make(map[FleetDuty]*spawnedDuty),
	}
}

// reconcile makes the running set match desired -- one action per diffed change.
func (p *dutyPool) reconcile(ctx context.Context, desired []FleetDuty) {
	for _, c := range p.diff(desired) {
		switch c.change {
		case dutyAdded:
			p.start(ctx, c.key)
		case dutyRemoved: // vanished from the table
			p.stop(ctx, c.key)
		case dutyExited:
			// shouldn't happen (duty errors are non-fatal), but a dead
			// goroutine must not keep holding the key. stop just clears the
			// corpse; then spawns a fresh runner.
			p.stop(ctx, c.key)
			p.start(ctx, c.key)
		}
	}
}

// diff compares desired against running and returns what reconcile must act on;
// keys running as desired produce no change.
func (p *dutyPool) diff(desired []FleetDuty) []dutyChange {
	want := make(map[FleetDuty]bool, len(desired))
	var changes []dutyChange

	for _, key := range desired {
		want[key] = true
		sd, running := p.running[key]
		switch {
		case !running:
			changes = append(changes, dutyChange{dutyAdded, key})
		case sd.finished():
			changes = append(changes, dutyChange{dutyExited, key})
		}
	}

	for key := range p.running {
		if !want[key] {
			changes = append(changes, dutyChange{dutyRemoved, key})
		}
	}

	return changes
}

// finished reports whether the duty's Run has returned.
func (sd *spawnedDuty) finished() bool {
	select {
	case <-sd.done:
		return true
	default:
		return false
	}
}

// start offers one row to the pool's duty list and runs the duty that
// accepts under its own child ctx. Errors warn -- the next refresh retries.
func (p *dutyPool) start(ctx context.Context, key FleetDuty) {
	owner, err := key.owner()
	if err != nil {
		p.logger.WarnContext(ctx, "fleet could not spawn duty -- retrying next refresh", "duty", key.Duty, "topic", key.TopicName, "group", key.ConsumerGroup, "error", err)
		return
	}
	meta := &key.Metadata // listDuties already read the row's metadata

	for _, construct := range p.duties {
		duty, err := construct(p.ds, p.config)
		if err == nil {
			var registered bool
			registered, err = duty.Register(ctx, key.Duty, owner, meta)
			if err == nil && !registered {
				continue // not this duty's kind -- offer the row to the next
			}
		}
		if err != nil {
			if ctx.Err() == nil {
				p.logger.WarnContext(ctx, "fleet could not spawn duty -- retrying next refresh", "duty", key.Duty, "topic", key.TopicName, "group", key.ConsumerGroup, "error", err)
			}
			return
		}

		dutyCtx, stop := context.WithCancel(ctx)
		sd := &spawnedDuty{stop: stop, done: make(chan struct{})}

		p.inflight.Go(func() {
			defer close(sd.done)
			if err := duty.Run(dutyCtx); err != nil {
				p.logger.ErrorContext(dutyCtx, "fleet duty exited", "duty", key.Duty, "topic", key.TopicName, "group", key.ConsumerGroup, "error", err)
			}
		})

		p.running[key] = sd
		p.logger.InfoContext(ctx, "fleet spawned duty", "duty", key.Duty, "topic", key.TopicName, "group", key.ConsumerGroup, "rate", key.Metadata.PollRate)
		return
	}

	p.logger.WarnContext(ctx, "no duty in the fleet's list runs this kind -- skipping", "duty", key.Duty, "topic", key.TopicName, "group", key.ConsumerGroup)
}

// stop cancels one duty and forgets it. The goroutine drains on its own
// time -- the WaitGroup keeps tracking it, so wait still covers it.
func (p *dutyPool) stop(ctx context.Context, key FleetDuty) {
	p.running[key].stop()
	delete(p.running, key)
	p.logger.InfoContext(ctx, "fleet stopped duty", "duty", key.Duty, "topic", key.TopicName, "group", key.ConsumerGroup)
}

// wait blocks until every spawned goroutine -- running or stopped -- has
// returned. Call only after the ctx every duty derives from is cancelled.
func (p *dutyPool) wait() {
	p.inflight.Wait()
}
