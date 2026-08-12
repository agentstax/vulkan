package alert

import (
	"context"
	"fmt"
)

const partitionCountName = "partition_count"

// PartitionCountJobName is the check's cron job name -- // job requests are produced with.
const PartitionCountJobName = "alert." + partitionCountName

const partitionCountSchedule = "@hourly"

// partitionCountWarnDivisor puts the warn threshold at half the lock ceiling --
// the ceiling is where Destroy starts failing, so warn leaves headroom to act.
const partitionCountWarnDivisor = 2

// runPartitionCount checks every topic's partition count against the
// lock-table ceiling where DROP/Destroy starts failing.
func runPartitionCount(ctx context.Context, ds *AlertDatastore, threshold int64) ([]*Alert, error) {
	ceiling, err := ds.partitionLockCeiling(ctx)
	if err != nil {
		return nil, err
	}
	topics, err := ds.topics(ctx)
	if err != nil {
		return nil, err
	}

	var alerts []*Alert
	for _, t := range topics {
		count, err := ds.partitionCount(ctx, t.id)
		if err != nil {
			return nil, err
		}
		if a := partitionCountAlert(t.id, t.name, count, ceiling, threshold); a != nil {
			alerts = append(alerts, a)
		}
	}
	return alerts, nil
}

// partitionCountAlert is the pure decision: nil unless count crosses the
// override, or half the live ceiling when no override is set.
func partitionCountAlert(topicId int64, topicName string, count, ceiling, threshold int64) *Alert {
	if threshold == 0 {
		threshold = ceiling / partitionCountWarnDivisor
	}
	if threshold <= 0 || count < threshold {
		return nil
	}

	// inputs are check-supplied and valid, so NewAlert can't fail here
	a, _ := NewAlert(
		partitionCountName, EntityTypeTopic, topicId, topicName, StatusActive, SeverityWarn,
		fmt.Sprintf("topic %q has %d partitions, approaching the lock-table ceiling (~%d)", topicName, count, ceiling),
		`Dropping or destroying the topic locks ~5 relations per partition in one transaction; past the ceiling that fails with "out of shared memory".`,
		"Lower the topic's retention so the janitor drops old partitions, or raise max_locks_per_transaction.",
		map[string]any{
			"partition_count": count,
			"lock_ceiling":    ceiling,
			"threshold":       threshold,
		},
		nil,
	)
	return a
}
