package metrics

import "context"

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
