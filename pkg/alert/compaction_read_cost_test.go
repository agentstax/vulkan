package alert

import "testing"

func TestCompactionReadCostAlertThreshold(t *testing.T) {
	if a := compactionReadCostAlert(1, "t", compactionReadCostWarnPartitions, 0); a == nil {
		t.Error("at threshold: want alert, got nil")
	}
	if a := compactionReadCostAlert(1, "t", compactionReadCostWarnPartitions-1, 0); a != nil {
		t.Error("below threshold: want nil")
	}
}
