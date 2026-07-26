package metrics

import (
	"context"
	"fmt"
	"time"

	consumermetrics "github.com/agentstax/vulkan/pkg/consumer/metrics"
	maintainmetrics "github.com/agentstax/vulkan/pkg/maintain/metrics"
)

// Snapshot is the full current picture across every composed metrics provider.
type Snapshot struct {
	QueueState        consumermetrics.ConsumerGroupSnapshot
	AbandonedRoutines consumermetrics.AbandonedRoutinesSnapshot
	Duties            []maintainmetrics.DutySnapshot
}

// Snapshot aggregates every provider's current picture in one live read.
func (m *Metrics) Snapshot(ctx context.Context) (*Snapshot, error) {
	queueState, err := m.QueueState.Snapshot(ctx)
	if err != nil {
		return nil, err
	}

	duties, err := m.DutyState.Snapshot(ctx)
	if err != nil {
		return nil, err
	}

	return &Snapshot{
		QueueState:        *queueState,
		AbandonedRoutines: m.AbandonedRoutines.Snapshot(),
		Duties:            duties,
	}, nil
}

func (s Snapshot) String() string {
	oldestUnacked := "none"
	if s.QueueState.ReadyExceptions+s.QueueState.InflightExceptions > 0 {
		// clamp negative -- clock skew between the DB and this process can put
		// created_at a few ms in this process's "future", never a real wait
		age := max(s.QueueState.OldestUnackedAge, 0)
		oldestUnacked = age.Round(time.Millisecond).String()
	}

	var overdue int
	var oldestGateAge time.Duration
	for i, duty := range s.Duties {
		if duty.Overdue {
			overdue++
		}
		if i == 0 || duty.GateAge > oldestGateAge {
			oldestGateAge = duty.GateAge
		}
	}

	return fmt.Sprintf(
		"queue:      head=%d claimed=%d committed=%d  (backlog=%d, inflight=%d)\n"+
			"exceptions: ready=%d inflight=%d dead=%d  (oldest unacked: %s)\n"+
			"leases:     open=%d\n"+
			"abandoned:  total=%d outstanding=%d  (avg self-clear: %s)\n"+
			"duties:     total=%d overdue=%d  (oldest gate age: %s)",
		s.QueueState.Head, s.QueueState.Claimed, s.QueueState.Committed, s.QueueState.Backlog, s.QueueState.Inflight,
		s.QueueState.ReadyExceptions, s.QueueState.InflightExceptions, s.QueueState.DeadExceptions, oldestUnacked,
		s.QueueState.OpenLeases,
		s.AbandonedRoutines.Total, s.AbandonedRoutines.Outstanding, s.AbandonedRoutines.SelfClearLatencyAvg.Round(time.Millisecond),
		len(s.Duties), overdue, oldestGateAge.Round(time.Millisecond),
	)
}
