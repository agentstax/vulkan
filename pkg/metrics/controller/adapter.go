package controller

import (
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/metrics"
	"github.com/agentstax/vulkan/pkg/metrics/controller/datastore"
)

// overdueThreshold: how long a cron job may sit due-but-unfired before it
// counts as overdue.
const overdueThreshold = 10 * time.Minute

// overdueFactor: how many rates past its gate a duty counts as overdue.
const overdueFactor = 10

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
		Owner:             owner,
		Name:              data.Name,
		Status:            classifyWorker(data.TargetInstances, data.LiveInstances),
		TargetInstances:   data.TargetInstances,
		LiveInstances:     data.LiveInstances,
		Attempts:          data.MaxAttempts,
		OldestInstanceAge: time.Duration(data.OldestInstanceAgeSecs * float64(time.Second)),
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

func toCronJobSnapshot(data datastore.CronJobSnapshotData) (metrics.CronJobSnapshot, error) {
	owner, err := toOwner(data.SystemId, data.TopicId, data.ConsumerGroupId, data.TopicName, data.GroupName)
	if err != nil {
		return metrics.CronJobSnapshot{}, err
	}

	snapshot := metrics.CronJobSnapshot{
		Owner:             owner,
		Name:              data.Name,
		Schedule:          data.Schedule,
		Suspended:         data.Suspended,
		NextScheduledTime: data.NextScheduledTime,
		DueFor:            time.Duration(data.DueForSecs * float64(time.Second)),
	}
	if data.LastScheduledTime.Valid {
		snapshot.LastScheduledTime = data.LastScheduledTime.Time
	}

	// a suspended row's next_scheduled_time goes stale on purpose --
	// unsuspending recomputes it, so staleness is never overdue
	snapshot.Overdue = !snapshot.Suspended && snapshot.DueFor > overdueThreshold
	return snapshot, nil
}

func toDutySnapshot(data datastore.DutySnapshotData) metrics.DutySnapshot {
	snapshot := metrics.DutySnapshot{
		Duty:          data.Duty,
		TopicName:     data.TopicName,
		ConsumerGroup: data.ConsumerGroup,
		Rate:          time.Duration(data.RateNs.Int64),
		GateAge:       time.Duration(data.GateAgeSecs * float64(time.Second)),
		Attempts:      data.Attempts,
	}
	snapshot.Overdue = snapshot.GateAge > overdueFactor*snapshot.Rate
	return snapshot
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
