package alert

import "testing"

func TestPartitionCountAlertThreshold(t *testing.T) {
	// ceiling 1000 -> live warn threshold 500
	if a := partitionCountAlert(1, "t", 499, 1000, 0); a != nil {
		t.Errorf("below threshold: want nil, got %+v", a)
	}
	if a := partitionCountAlert(1, "t", 500, 1000, 0); a == nil {
		t.Error("at threshold: want alert, got nil")
	}
	// override beats the live threshold
	if a := partitionCountAlert(1, "t", 100, 1000, 50); a == nil {
		t.Error("override honored: want alert, got nil")
	}
}
