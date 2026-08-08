package datastore

import (
	"context"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
)

// WorkerSnapshots is every worker row's current claim state, queried live
// from Postgres -- works cold, nothing needs to be running.
func (d *MetricsDatastore) WorkerSnapshots(ctx context.Context) ([]WorkerSnapshot, error) {
	var workers []WorkerSnapshot
	err := d.Retry.Wrap(ctx, func() error {
		var err error
		workers, err = d.workerSnapshots(ctx)
		return err
	})
	return workers, err
}

func (d *MetricsDatastore) workerSnapshots(ctx context.Context) ([]WorkerSnapshot, error) {
	sql := `
		SELECT
			w.name,
			COALESCE(w.system_id, t.system_id, 0),                        -- w.system_id is NULL unless system-owned
			COALESCE(w.topic_id, g.topic_id, 0),                          -- w.topic_id is NULL unless topic-owned
			COALESCE(w.consumer_group_id, 0),                             -- w.consumer_group_id is NULL unless group-owned
			COALESCE(t.name, ''),
			COALESCE(g.name, ''),
			w.target_instances,
			COUNT(i.id) FILTER (WHERE i.expires_at > now()) AS live_instances,
			COALESCE(MAX(i.attempts) FILTER (WHERE i.expires_at > now()), 0) AS max_attempts,
			COALESCE(EXTRACT(EPOCH FROM (now() - MIN(i.created_at) FILTER (WHERE i.expires_at > now()))), 0) AS oldest_instance_age_secs,
			COALESCE(EXTRACT(EPOCH FROM (now() - MAX(i.expires_at))), 0) AS unclaimed_for_secs  -- dead rows feed this until something deletes them
		FROM worker w
		LEFT JOIN consumer_group g ON g.id = w.consumer_group_id
		LEFT JOIN topic t ON t.id = COALESCE(w.topic_id, g.topic_id)      -- group rows reach their topic through the group
		LEFT JOIN worker_instance i ON i.worker_id = w.id
		GROUP BY w.id, w.name, w.system_id, w.topic_id, w.consumer_group_id, w.target_instances, t.system_id, t.name, g.topic_id, g.name
		ORDER BY t.name, w.name, g.name;
	`

	rows, err := d.Datastore.Pool.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var workers []WorkerSnapshot
	for rows.Next() {
		var s WorkerSnapshot
		var systemId, topicId, consumerGroupId int64
		var topicName, groupName string
		var oldestInstanceAgeSecs, newestExpirySecs float64

		if err := rows.Scan(&s.Name, &systemId, &topicId, &consumerGroupId, &topicName, &groupName,
			&s.TargetInstances, &s.LiveInstances, &s.Attempts, &oldestInstanceAgeSecs, &newestExpirySecs); err != nil {
			return nil, err
		}

		s.Owner, err = toWorkerOwner(systemId, topicId, consumerGroupId, topicName, groupName)
		if err != nil {
			return nil, err
		}

		s.Status = classifyWorker(s.TargetInstances, s.LiveInstances)

		s.OldestInstanceAge = time.Duration(oldestInstanceAgeSecs * float64(time.Second))

		if s.LiveInstances == 0 && newestExpirySecs > 0 {
			s.UnclaimedFor = time.Duration(newestExpirySecs * float64(time.Second))
		}

		workers = append(workers, s)
	}
	return workers, rows.Err()
}

func classifyWorker(targetInstances int, liveInstances int) WorkerStatus {
	switch {
	case targetInstances == 0:
		return WorkerSuspended
	case liveInstances > 0:
		return WorkerClaimed
	default:
		return WorkerUnclaimed
	}
}

func toWorkerOwner(systemId int64, topicId int64, consumerGroupId int64, topicName string, groupName string) (*common.Owner, error) {
	switch {
	case consumerGroupId > 0:
		return common.NewConsumerGroupOwner(systemId, topicId, consumerGroupId, groupName)
	case topicId > 0:
		return common.NewTopicOwner(systemId, topicId, topicName)
	default:
		return common.NewSystemOwner(systemId)
	}
}
