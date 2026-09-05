package controller

import (
	"fmt"
	"time"

	"github.com/agentstax/vulkan/pkg/alert"
	"github.com/agentstax/vulkan/pkg/common"
)

// The crossing decision is the caller's -- an alert built below threshold is
// a bug.
func newCompactionReadCostAlert(owner *common.Owner, count int64, threshold int64, at time.Time) (*alert.Alert, error) {
	if threshold <= 0 {
		return nil, fmt.Errorf("threshold must be > 0, got %d", threshold)
	}
	if count < threshold {
		return nil, fmt.Errorf("count %d is below threshold %d", count, threshold)
	}

	message := fmt.Sprintf("compacted topic %q has %d partitions; latest-key replay cost grows ~10µs per partition", owner.Name, count)
	detail := "A consumer replaying a never-superseded key scans from that key's partition to the current tail; the cost grows linearly with partition count and never amortizes."
	hint := "Compact more aggressively or lower retention so old partitions drop, bounding replay cost."
	data := map[string]any{
		"partition_count": count,
		"threshold":       threshold,
	}
	return alert.NewAlert(alert.AlertCompactionReadCost.Name, owner, alert.AlertStatusActive, alert.AlertSeverity(alert.AlertCompactionReadCost.Severity), message, at, &alert.AlertOptions{
		Detail: detail,
		Hint:   hint,
		Data:   data,
	})
}
