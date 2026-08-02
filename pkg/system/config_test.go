package system

import (
	"testing"
	"time"
)

func TestConfigWithDefaults(t *testing.T) {
	// zero values resolve to the documented defaults
	c := (&Config{}).WithDefaults()
	if c.AlertRepeatInterval != 4*time.Hour {
		t.Errorf("AlertRepeatInterval default = %v, want 4h", c.AlertRepeatInterval)
	}

	// caller-set values survive WithDefaults
	set := (&Config{AlertRepeatInterval: time.Hour}).WithDefaults()
	if set.AlertRepeatInterval != time.Hour {
		t.Errorf("AlertRepeatInterval = %v, want 1h", set.AlertRepeatInterval)
	}
}

func TestConfigValidate(t *testing.T) {
	if err := (&Config{AlertRepeatInterval: -1}).WithDefaults().Validate(); err == nil {
		t.Error("negative AlertRepeatInterval: want error, got nil")
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
	if err := (&AlterConfig{AlertRepeatInterval: &neg}).Validate(); err == nil {
		t.Error("non-positive AlertRepeatInterval: want error, got nil")
	}

	ok := 5 * time.Minute
	if err := (&AlterConfig{AlertRepeatInterval: &ok}).Validate(); err != nil {
		t.Errorf("valid single-field patch: want nil, got %v", err)
	}
}

func TestNewSystem(t *testing.T) {
	now := time.Unix(0, 0)
	if _, err := NewSystem(0, time.Hour, now, now); err == nil {
		t.Error("non-positive id: want error, got nil")
	}
	if _, err := NewSystem(1, -1, now, now); err == nil {
		t.Error("negative alertRepeatInterval: want error, got nil")
	}

	s, err := NewSystem(1, 4*time.Hour, now, now)
	if err != nil {
		t.Fatalf("valid: want nil, got %v", err)
	}
	if s.AlertRepeatInterval != 4*time.Hour {
		t.Errorf("NewSystem stored %+v", s)
	}
}
