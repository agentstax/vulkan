package alert

import (
	"testing"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
)

func TestNewAlertCarriesAt(t *testing.T) {
	owner, err := common.NewTopicOwner(1, 41, "payments.requested")
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 9, 5, 10, 3, 0, 0, time.UTC)

	built, err := NewAlert(AlertPartitionCount.Name, owner, AlertStatusActive, AlertSeverityWarn, "condition holds", at, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !built.At.Equal(at) {
		t.Fatalf("at = %v, want %v", built.At, at)
	}
}

func TestNewAlertRejectsZeroAt(t *testing.T) {
	owner, err := common.NewTopicOwner(1, 41, "payments.requested")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := NewAlert(AlertPartitionCount.Name, owner, AlertStatusActive, AlertSeverityWarn, "condition holds", time.Time{}, nil); err == nil {
		t.Fatal("NewAlert accepted a zero at")
	}
}
