package metrics

import "context"

// Snapshot is the full current picture of this consumer's metrics.
type Snapshot struct {
	QueueState        QueueState
	AbandonedRoutines AbandonedRoutinesSnapshot
}

func (m *ConsumerMetrics) Snapshot(ctx context.Context) (*Snapshot, error) {
	queueState, err := m.datastore.QueueState(ctx, m.topicID, m.group)
	if err != nil {
		return nil, err
	}

	return &Snapshot{
		QueueState:        *queueState,
		AbandonedRoutines: m.AbandonedRoutines.Snapshot(),
	}, nil
}
