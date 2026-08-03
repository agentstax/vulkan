package manager

import (
	"context"
	"sync"

	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/worker"
)

// instancePool supervises the goroutine behind every instance the manager
// instance has spawned.
type instancePool struct {
	logger    logger.Logger
	factories map[string]worker.Factory  // keyed by Name, copied from the manager at construction
	running   map[int64]*spawnedInstance // keyed by worker row id
	inflight  sync.WaitGroup             // every spawned Run goroutine, including stopped ones still draining
}

type spawnedInstance struct {
	stop context.CancelFunc
	done chan struct{} // closed when the instance's Run returns

	worker string // for the stop log -- the row itself is gone by then
	owner  string
}

// changeType classifies one worker row's difference between the desired set
// and the current running set.
type changeType int

const (
	workerAdded   changeType = iota // desired but not running
	workerRemoved                   // running but no longer desired
	workerExited                    // running, but its Run goroutine already returned
)

type workerChange struct {
	change changeType
	id     int64
	worker *worker.Worker // nil on workerRemoved -- the row is gone
}

func newInstancePool(log logger.Logger, factories map[string]worker.Factory) *instancePool {
	return &instancePool{
		logger:    log,
		factories: factories,
		running:   make(map[int64]*spawnedInstance),
	}
}

// reconcile makes the running set match desired -- one action per diffed change.
func (p *instancePool) reconcile(ctx context.Context, desiredWorkers []*worker.Worker) {
	for _, change := range p.diff(desiredWorkers) {
		switch change.change {
		case workerAdded:
			p.start(ctx, change.worker)
		case workerRemoved: // vanished from the table
			p.stop(ctx, change.id)
		case workerExited:
			// an instance exit is not fatal, but a dead goroutine must not
			// keep holding the map entry. stop clears the corpse; then a
			// fresh instance spawns.
			p.stop(ctx, change.id)
			p.start(ctx, change.worker)
		}
	}
}

// diff compares desired against running and returns what reconcile must act
// on; workers running as desired produce no change.
func (p *instancePool) diff(desiredWorkers []*worker.Worker) []workerChange {
	want := make(map[int64]bool, len(desiredWorkers))
	var changes []workerChange

	for _, desiredWorker := range desiredWorkers {
		want[desiredWorker.Id] = true
		spawned, running := p.running[desiredWorker.Id]
		switch {
		case !running:
			changes = append(changes, workerChange{change: workerAdded, id: desiredWorker.Id, worker: desiredWorker})
		case spawned.finished():
			changes = append(changes, workerChange{change: workerExited, id: desiredWorker.Id, worker: desiredWorker})
		}
	}

	for id := range p.running {
		if !want[id] {
			changes = append(changes, workerChange{change: workerRemoved, id: id})
		}
	}

	return changes
}

// finished reports whether the instance's Run has returned.
func (i *spawnedInstance) finished() bool {
	select {
	case <-i.done:
		return true
	default:
		return false
	}
}

// start spawns one worker row through its factory under its own child ctx.
// Errors warn -- the next reconcile retries.
func (p *instancePool) start(ctx context.Context, worker *worker.Worker) {
	factory, ok := p.factories[worker.Name]
	if !ok {
		// expected every pass, not a misconfiguration -- a chain carries rows
		// the manager has no factory for, its own manager row at minimum
		p.logger.DebugContext(ctx, "no factory in the manager's list runs this worker -- skipping", "worker", worker.Name, "owner", worker.Owner.Name)
		return
	}

	instance, err := factory.Register(ctx, worker.Id, &worker.Owner, worker.Metadata)
	if err != nil {
		if ctx.Err() == nil {
			p.logger.WarnContext(ctx, "manager could not spawn worker -- retrying next reconcile", "worker", worker.Name, "owner", worker.Owner.Name, "error", err)
		}
		return
	}
	if instance == nil {
		// declined: target_instances is already filled, likely by another
		// replica -- the next reconcile tries again
		p.logger.DebugContext(ctx, "worker declined an instance", "worker", worker.Name, "owner", worker.Owner.Name)
		return
	}

	instanceCtx, stop := context.WithCancel(ctx)
	spawned := &spawnedInstance{stop: stop, done: make(chan struct{}), worker: worker.Name, owner: worker.Owner.Name}

	p.inflight.Go(func() {
		defer close(spawned.done)
		if err := instance.Run(instanceCtx); err != nil {
			p.logger.ErrorContext(instanceCtx, "worker exited", "worker", spawned.worker, "owner", spawned.owner, "error", err)
		}
	})

	p.running[worker.Id] = spawned
	p.logger.InfoContext(ctx, "manager spawned worker", "worker", worker.Name, "owner", worker.Owner.Name)
}

// stop cancels one instance and forgets it. The goroutine drains on its own
// time -- the WaitGroup keeps tracking it, so wait still covers it.
func (p *instancePool) stop(ctx context.Context, id int64) {
	spawned := p.running[id]
	spawned.stop()
	delete(p.running, id)
	p.logger.InfoContext(ctx, "manager stopped worker", "worker", spawned.worker, "owner", spawned.owner)
}

// wait blocks until every spawned goroutine -- running or stopped -- has
// returned. Call only after the ctx every instance derives from is cancelled.
func (p *instancePool) wait() {
	p.inflight.Wait()
}
