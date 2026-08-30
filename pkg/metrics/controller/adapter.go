package controller

import (
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/metrics"
	"github.com/agentstax/vulkan/pkg/metrics/controller/datastore"
)

// overdueThreshold: how long a schedule may sit due and unproduced before it
// counts as overdue.
const overdueThreshold = 10 * time.Minute

func toOwner(systemId int64, topicId int64, consumerGroupId int64, topicName string, groupName string) (*common.Owner, error) {
	switch {
	case consumerGroupId > 0:
		return common.NewConsumerGroupOwner(systemId, topicId, consumerGroupId, groupName)
	case topicId > 0:
		return common.NewTopicOwner(systemId, topicId, topicName)
	default:
		return common.NewSystemOwner(systemId)
	}
}

func toWorkerSnapshot(data datastore.WorkerSnapshotData) (metrics.WorkerSnapshot, error) {
	owner, err := toOwner(data.SystemId, data.TopicId, data.ConsumerGroupId, data.TopicName, data.GroupName)
	if err != nil {
		return metrics.WorkerSnapshot{}, err
	}

	snapshot := metrics.WorkerSnapshot{
		Owner:           owner,
		Name:            data.Name,
		Status:          classifyWorker(data.TargetInstances, data.LiveInstances),
		TargetInstances: data.TargetInstances,
		LiveInstances:   data.LiveInstances,
		Attempts:        data.MaxAttempts,
	}
	if data.LiveInstances == 0 && data.UnclaimedForSecs > 0 {
		snapshot.UnclaimedFor = time.Duration(data.UnclaimedForSecs * float64(time.Second))
	}
	return snapshot, nil
}

func classifyWorker(targetInstances int, liveInstances int) metrics.WorkerStatus {
	switch {
	case targetInstances == 0:
		return metrics.WorkerSuspended
	case liveInstances > 0:
		return metrics.WorkerClaimed
	default:
		return metrics.WorkerUnclaimed
	}
}

func toScheduleSnapshot(data datastore.ScheduleSnapshotData) (metrics.ScheduleSnapshot, error) {
	owner, err := common.NewSystemOwner(data.SystemId)
	if err != nil {
		return metrics.ScheduleSnapshot{}, err
	}

	snapshot := metrics.ScheduleSnapshot{
		Owner:           owner,
		Name:            data.Name,
		Topic:           data.TopicName,
		Expression:      data.Expression,
		Suspended:       data.Suspended,
		NextScheduledAt: data.NextScheduledAt,
		DueFor:          time.Duration(data.DueForSecs * float64(time.Second)),
	}
	if data.LastScheduledAt != nil {
		snapshot.LastScheduledAt = *data.LastScheduledAt
	}

	// a suspended row's next_scheduled_at goes stale on purpose --
	// unsuspending recomputes it, so staleness is never overdue
	snapshot.Overdue = !snapshot.Suspended && snapshot.DueFor > overdueThreshold
	return snapshot, nil
}

func toConsumerGroupSnapshot(consumerGroup string, data *datastore.ConsumerGroupSnapshotData) *metrics.ConsumerGroupSnapshot {
	snapshot := &metrics.ConsumerGroupSnapshot{
		ConsumerGroup: consumerGroup,
		Cursor: metrics.CursorSnapshot{
			Head:      data.Head,
			Claimed:   data.Claimed,
			Committed: data.Committed,
			Backlog:   data.Head - data.Committed,
			Inflight:  data.Claimed - data.Committed,
		},
		Exceptions: metrics.ExceptionSnapshot{
			Ready:    data.ReadyExceptions,
			Inflight: data.InflightExceptions,
			Deferred: data.DeferredExceptions,
			Dead:     data.DeadExceptions,
		},
		OpenLeases: data.OpenLeases,
	}
	if data.OldestUnresolvedAt != nil {
		snapshot.Exceptions.OldestUnresolvedAge = time.Since(*data.OldestUnresolvedAt)
	}
	return snapshot
}

func toAbandonedRoutineSnapshot(abandoned []datastore.EventTimestampData, cleared []datastore.EventTimestampData) *metrics.AbandonedRoutineSnapshot {
	// eventKey is the (message, attempt) identity an abandoned event and its
	// matching cleared event share -- topicId/group are already fixed by the
	// routing key both reads filter on, so they're not part of the key.
	type eventKey struct {
		MessageId int64
		Attempt   int
	}

	clearedAt := make(map[eventKey]time.Time, len(cleared))
	for _, event := range cleared {
		clearedAt[eventKey{MessageId: event.MessageId, Attempt: event.Attempt}] = event.At
	}

	var snapshot metrics.AbandonedRoutineSnapshot
	var latencySum time.Duration
	var matched int64
	for _, event := range abandoned {
		snapshot.Total++
		at, ok := clearedAt[eventKey{MessageId: event.MessageId, Attempt: event.Attempt}]
		if !ok {
			snapshot.Outstanding++
			continue
		}
		latencySum += at.Sub(event.At)
		matched++
	}
	if matched > 0 {
		snapshot.SelfClearLatencyAvg = latencySum / time.Duration(matched)
	}
	return &snapshot
}

func toSchemaVersionSnapshot(count *datastore.SchemaVersionCountData, lags []datastore.GroupSchemaVersionLagData) metrics.SchemaVersionSnapshot {
	groups := make([]metrics.GroupSchemaVersionLag, 0, len(lags))
	for _, lag := range lags {
		groups = append(groups, metrics.GroupSchemaVersionLag{
			ConsumerGroup:        lag.ConsumerGroup,
			Unconsumed:           lag.Unconsumed,
			UnresolvedExceptions: lag.UnresolvedExceptions,
		})
	}
	return metrics.SchemaVersionSnapshot{
		Version:         int(count.SchemaVersion),
		Messages:        count.Messages,
		CompactionHeads: count.CompactionHeads,
		Groups:          groups,
	}
}
