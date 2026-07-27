package system

import (
	"testing"
	"time"
)

func TestConfigWithDefaults(t *testing.T) {
	// zero values resolve to the documented defaults
	c := (&Config{}).WithDefaults()
	if c.AdvisorPollRate != 2*time.Minute {
		t.Errorf("AdvisorPollRate default = %v, want 2m", c.AdvisorPollRate)
	}
	if c.AdvisoryRepeatInterval != 4*time.Hour {
		t.Errorf("AdvisoryRepeatInterval default = %v, want 4h", c.AdvisoryRepeatInterval)
	}

	// caller-set values survive WithDefaults
	set := (&Config{AdvisorPollRate: 30 * time.Second, AdvisoryRepeatInterval: time.Hour}).WithDefaults()
	if set.AdvisorPollRate != 30*time.Second {
		t.Errorf("AdvisorPollRate = %v, want 30s", set.AdvisorPollRate)
	}
	if set.AdvisoryRepeatInterval != time.Hour {
		t.Errorf("AdvisoryRepeatInterval = %v, want 1h", set.AdvisoryRepeatInterval)
	}
}

func TestConfigValidate(t *testing.T) {
	if err := (&Config{AdvisorPollRate: -1}).Validate(); err == nil {
		t.Error("negative AdvisorPollRate: want error, got nil")
	}
	if err := (&Config{AdvisoryRepeatInterval: -1}).Validate(); err == nil {
		t.Error("negative AdvisoryRepeatInterval: want error, got nil")
	}
	if err := (&Config{}).WithDefaults().Validate(); err != nil {
		t.Errorf("defaulted config: want nil, got %v", err)
	}
}

func TestAlterConfigValidate(t *testing.T) {
	if err := (&AlterConfig{}).Validate(); err == nil {
		t.Error("empty patch: want error (must change at least one field), got nil")
	}

	neg := -1 * time.Second
	if err := (&AlterConfig{AdvisorPollRate: &neg}).Validate(); err == nil {
		t.Error("non-positive AdvisorPollRate: want error, got nil")
	}
	if err := (&AlterConfig{AdvisoryRepeatInterval: &neg}).Validate(); err == nil {
		t.Error("non-positive AdvisoryRepeatInterval: want error, got nil")
	}

	ok := 5 * time.Minute
	if err := (&AlterConfig{AdvisorPollRate: &ok}).Validate(); err != nil {
		t.Errorf("valid single-field patch: want nil, got %v", err)
	}
}

func TestNewSystem(t *testing.T) {
	now := time.Unix(0, 0)
	if _, err := NewSystem(-1, time.Hour, now, now); err == nil {
		t.Error("negative advisorPollRate: want error, got nil")
	}
	if _, err := NewSystem(time.Minute, -1, now, now); err == nil {
		t.Error("negative advisoryRepeatInterval: want error, got nil")
	}

	s, err := NewSystem(2*time.Minute, 4*time.Hour, now, now)
	if err != nil {
		t.Fatalf("valid: want nil, got %v", err)
	}
	if s.AdvisorPollRate != 2*time.Minute || s.AdvisoryRepeatInterval != 4*time.Hour {
		t.Errorf("NewSystem stored %+v", s)
	}
}
