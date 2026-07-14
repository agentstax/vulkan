package metrics

import (
	"context"
	"fmt"
	"time"
)

// Snapshot is the full current picture of this consumer's metrics.
type Snapshot struct {
	QueueState        QueueStateSnapshot
	AbandonedRoutines AbandonedRoutinesSnapshot
}

func (m *ConsumerMetrics) Snapshot(ctx context.Context) (*Snapshot, error) {
	queueState, err := m.QueueState.Snapshot(ctx)
	if err != nil {
		return nil, err
	}

	return &Snapshot{
		QueueState:        *queueState,
		AbandonedRoutines: m.AbandonedRoutines.Snapshot(),
	}, nil
}

// String formats the snapshot for on demand prints
func (s Snapshot) String() string {
	oldestUnacked := "none"
	if s.QueueState.ReadyExceptions+s.QueueState.InflightExceptions > 0 {
		// clamp negative -- clock skew between the DB and this process can put
		// created_at a few ms in this process's "future", never a real wait
		age := max(s.QueueState.OldestUnackedAge, 0)
		oldestUnacked = age.Round(time.Millisecond).String()
	}

	return fmt.Sprintf(
		"queue:      head=%d claimed=%d committed=%d  (backlog=%d, inflight=%d)\n"+
			"exceptions: ready=%d inflight=%d dead=%d  (oldest unacked: %s)\n"+
			"leases:     open=%d\n"+
			"abandoned:  total=%d outstanding=%d  (avg self-clear: %s)",
		s.QueueState.Head, s.QueueState.Claimed, s.QueueState.Committed, s.QueueState.Backlog, s.QueueState.Inflight,
		s.QueueState.ReadyExceptions, s.QueueState.InflightExceptions, s.QueueState.DeadExceptions, oldestUnacked,
		s.QueueState.OpenLeases,
		s.AbandonedRoutines.Total, s.AbandonedRoutines.Outstanding, s.AbandonedRoutines.SelfClearLatencyAvg.Round(time.Millisecond),
	)
}
