package manager

import (
	"context"
	"errors"
	"fmt"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/worker"
	"golang.org/x/sync/errgroup"
)

// executionPool supervises the goroutine behind every execution the manager
// has spawned.
type executionPool struct {
	logger       common.Logger
	provisioners map[string]worker.Provisioner // keyed by Name, copied from the manager at construction
	running      map[int64]*spawnedExecution   // keyed by worker row id
	group        *errgroup.Group               // every spawned Run goroutine; its first fatal error cancels the manager's run
}

func newExecutionPool(provisioners map[string]worker.Provisioner, group *errgroup.Group, log common.Logger) (*executionPool, error) {
	if provisioners == nil {
		return nil, errors.New("provisioners must not be nil")
	}
	if group == nil {
		return nil, errors.New("group must not be nil")
	}
	if log == nil {
		return nil, errors.New("logger must not be nil")
	}

	return &executionPool{
		logger:       log,
		provisioners: provisioners,
		running:      make(map[int64]*spawnedExecution),
		group:        group,
	}, nil
}

type spawnedExecution struct {
	stop context.CancelFunc
	done chan struct{} // closed when the execution's Run returns

	worker string // for the stop log -- the row itself is gone by then
	owner  string
}

func newSpawnedExecution(stop context.CancelFunc, workerName string, ownerName string) (*spawnedExecution, error) {
	if stop == nil {
		return nil, errors.New("stop must not be nil")
	}
	return &spawnedExecution{
		stop:   stop,
		done:   make(chan struct{}),
		worker: workerName,
		owner:  ownerName,
	}, nil
}

// finished reports whether the execution's Run has returned.
func (s *spawnedExecution) finished() bool {
	select {
	case <-s.done:
		return true
	default:
		return false
	}
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

// newWorkerChange rejects a pairing diff can never produce: workerRemoved is
// the one change whose row is already gone.
func newWorkerChange(change changeType, id int64, desiredWorker *worker.Worker) (workerChange, error) {
	if id <= 0 {
		return workerChange{}, fmt.Errorf("id must be > 0, got %d", id)
	}
	if (change == workerRemoved) != (desiredWorker == nil) {
		return workerChange{}, errors.New("desiredWorker must be nil exactly when the change is workerRemoved")
	}

	return workerChange{change: change, id: id, worker: desiredWorker}, nil
}

// reconcile makes the running set match desired -- one action per diffed change.
func (p *executionPool) reconcile(ctx context.Context, desiredWorkers []*worker.Worker) error {
	changes, err := p.diff(desiredWorkers)
	if err != nil {
		return err
	}
	for _, change := range changes {
		switch change.change {
		case workerAdded:
			p.start(ctx, change.worker)
		case workerRemoved: // vanished from the table
			p.stop(ctx, change.id)
		case workerExited:
			// an execution exit is not fatal, but a dead goroutine must not
			// keep holding the map entry. stop clears the corpse; then a
			// fresh execution spawns.
			p.stop(ctx, change.id)
			p.start(ctx, change.worker)
		}
	}
	return nil
}

// diff compares desired against running and returns what reconcile must act
// on; workers running as desired produce no change.
func (p *executionPool) diff(desiredWorkers []*worker.Worker) ([]workerChange, error) {
	want := make(map[int64]bool, len(desiredWorkers))
	var changes []workerChange

	for _, desiredWorker := range desiredWorkers {
		want[desiredWorker.Id] = true
		spawned, running := p.running[desiredWorker.Id]

		var change workerChange
		var err error
		switch {
		case !running:
			change, err = newWorkerChange(workerAdded, desiredWorker.Id, desiredWorker)
		case spawned.finished():
			change, err = newWorkerChange(workerExited, desiredWorker.Id, desiredWorker)
		default:
			continue // running as desired
		}
		if err != nil {
			return nil, err
		}
		changes = append(changes, change)
	}

	for id := range p.running {
		if !want[id] {
			change, err := newWorkerChange(workerRemoved, id, nil)
			if err != nil {
				return nil, err
			}
			changes = append(changes, change)
		}
	}

	return changes, nil
}

// start spawns one worker row through its provisioner under its own child ctx.
// Errors warn -- the next reconcile retries.
func (p *executionPool) start(ctx context.Context, desiredWorker *worker.Worker) {
	provisioner, ok := p.provisioners[desiredWorker.Name]
	if !ok {
		// expected every pass, not a misconfiguration -- a chain carries rows
		// the manager has no provisioner for, its own manager row at minimum
		p.logger.DebugContext(ctx, "no provisioner in the manager's list runs this worker -- skipping", "worker", desiredWorker.Name, "owner", desiredWorker.Owner.Name)
		return
	}

	execution, err := provisioner.Provision(ctx, desiredWorker.Id, desiredWorker.Owner, desiredWorker.Metadata)
	if err != nil {
		if ctx.Err() == nil {
			p.logger.WarnContext(ctx, "manager could not spawn worker -- retrying next reconcile", "worker", desiredWorker.Name, "owner", desiredWorker.Owner.Name, "error", err)
		}
		return
	}
	if execution == nil {
		// declined: target_instances is already filled, likely by another
		// replica -- the next reconcile tries again
		p.logger.DebugContext(ctx, "worker declined an instance", "worker", desiredWorker.Name, "owner", desiredWorker.Owner.Name)
		return
	}

	executionCtx, stop := context.WithCancel(ctx)
	spawned, err := newSpawnedExecution(stop, desiredWorker.Name, desiredWorker.Owner.Name)
	if err != nil {
		stop()
		p.logger.WarnContext(ctx, "manager could not track spawned worker -- retrying next reconcile", "worker", desiredWorker.Name, "owner", desiredWorker.Owner.Name, "error", err)
		return
	}

	// pure interpreter of the execution's exit declaration, no policy of its own
	p.group.Go(func() error {
		defer close(spawned.done)

		// run is blocking
		err := execution.Run(executionCtx)

		switch {
		case err == nil, errors.Is(err, worker.ErrInstanceLost), executionCtx.Err() != nil:
			return nil // reconcile respawns / shutdown unwinding
		default:
			return fmt.Errorf("%s worker (%s): %w", spawned.worker, spawned.owner, err) // errgroup cancels -> manager down
		}
	})

	p.running[desiredWorker.Id] = spawned
	p.logger.InfoContext(ctx, "manager spawned worker", "worker", desiredWorker.Name, "owner", desiredWorker.Owner.Name)
}

// stop cancels one execution and forgets it. The goroutine drains on its own
// time -- the group keeps tracking it, so Wait still covers it.
func (p *executionPool) stop(ctx context.Context, id int64) {
	spawned, ok := p.running[id]
	if !ok {
		p.logger.WarnContext(ctx, "manager has no running execution for worker row -- nothing to stop", "worker_id", id)
		return
	}
	spawned.stop()
	delete(p.running, id)
	p.logger.InfoContext(ctx, "manager stopped worker", "worker", spawned.worker, "owner", spawned.owner)
}
