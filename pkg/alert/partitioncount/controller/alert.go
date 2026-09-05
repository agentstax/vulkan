package controller

import (
	"fmt"
	"time"

	"github.com/agentstax/vulkan/pkg/alert"
	"github.com/agentstax/vulkan/pkg/common"
)

// The crossing decision is the caller's -- an alert built below threshold is
// a bug.
func newPartitionCountAlert(owner *common.Owner, count int64, ceiling int64, threshold int64, at time.Time) (*alert.Alert, error) {
	if threshold <= 0 {
		return nil, fmt.Errorf("threshold must be > 0, got %d", threshold)
	}
	if count < threshold {
		return nil, fmt.Errorf("count %d is below threshold %d", count, threshold)
	}

	message := fmt.Sprintf("topic %q has %d partitions, approaching the lock-table ceiling (~%d)", owner.Name, count, ceiling)
	detail := `Dropping or destroying the topic locks ~5 relations per partition in one transaction; past the ceiling that fails with "out of shared memory".`
	hint := "Lower the topic's retention so the janitor drops old partitions, or raise max_locks_per_transaction."
	data := map[string]any{
		"partition_count": count,
		"lock_ceiling":    ceiling,
		"threshold":       threshold,
	}
	return alert.NewAlert(alert.AlertPartitionCount.Name, owner, alert.AlertStatusActive, alert.AlertSeverity(alert.AlertPartitionCount.Severity), message, at, &alert.AlertOptions{
		Detail: detail,
		Hint:   hint,
		Data:   data,
	})
}
