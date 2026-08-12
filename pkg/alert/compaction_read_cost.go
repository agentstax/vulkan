package alert

import (
	"context"
	"fmt"
)

const compactionReadCostName = "compaction_read_cost"

// CompactionReadCostJobName is the check's cron job name -- // job requests are produced with.
const CompactionReadCostJobName = "alert." + compactionReadCostName

const compactionReadCostSchedule = "@hourly"

// compactionReadCostWarnPartitions: ~10µs fixed cost per partition on every
// never-superseded key's replay, so warn where a full-backlog replay of one key
// crosses ~100ms.
const compactionReadCostWarnPartitions = 10_000

// runCompactionReadCost checks every compacted topic's partition count, which
// grows the per-key latest-replay cost.
func runCompactionReadCost(ctx context.Context, ds *AlertDatastore, threshold int64) ([]*Alert, error) {
	topics, err := ds.topics(ctx)
	if err != nil {
		return nil, err
	}

	var alerts []*Alert
	for _, t := range topics {
		compacted, err := ds.compacted(ctx, t.id)
		if err != nil {
			return nil, err
		}
		if !compacted {
			continue // only compacted topics carry this cost
		}
		count, err := ds.partitionCount(ctx, t.id)
		if err != nil {
			return nil, err
		}
		if a := compactionReadCostAlert(t.id, t.name, count, threshold); a != nil {
			alerts = append(alerts, a)
		}
	}
	return alerts, nil
}

// compactionReadCostAlert is the pure firing decision: nil unless count crosses
// the override, or the default warn partition count when no override is set.
func compactionReadCostAlert(topicId int64, topicName string, count, threshold int64) *Alert {
	if threshold == 0 {
		threshold = compactionReadCostWarnPartitions
	}
	if count < threshold {
		return nil
	}

	// inputs are check-supplied and valid, so NewAlert can't fail here
	a, _ := NewAlert(
		compactionReadCostName, EntityTypeTopic, topicId, topicName, StatusFiring, SeverityWarn,
		fmt.Sprintf("compacted topic %q has %d partitions; latest-key replay cost grows ~10µs per partition", topicName, count),
		"A consumer replaying a never-superseded key scans from that key's partition to the current tail; the cost grows linearly with partition count and never amortizes.",
		"Compact more aggressively or lower retention so old partitions drop, bounding replay cost.",
		map[string]any{
			"partition_count": count,
			"threshold":       threshold,
		},
		nil,
	)
	return a
}
