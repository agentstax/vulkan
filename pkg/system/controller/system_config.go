package controller

import (
	"errors"
	"fmt"
	"time"
)

// SystemConfig is RegisterSystem's spec -- control-plane settings scoped to no
// single topic.
type SystemConfig struct {
	// AlertRepeatInterval - how long an active alert stays quiet before it
	// repeats as a reminder. Default 4h.
	AlertRepeatInterval time.Duration
}

func (c *SystemConfig) WithDefaults() *SystemConfig {
	if c.AlertRepeatInterval == 0 {
		c.AlertRepeatInterval = 4 * time.Hour
	}
	return c
}

// Validate runs after WithDefaults -- anything still out of range here was set
// by the caller, not left unset.
func (c *SystemConfig) Validate() error {
	if c.AlertRepeatInterval < 0 {
		return fmt.Errorf("AlertRepeatInterval must be >= 0, got %v", c.AlertRepeatInterval)
	}
	return nil
}

// AlterSystemConfig is AlterSystem's sparse patch -- a nil field means leave unchanged.
type AlterSystemConfig struct {
	AlertRepeatInterval *time.Duration
}

func (c *AlterSystemConfig) Validate() error {
	if c.AlertRepeatInterval == nil {
		return errors.New("no fields set -- an alter must change at least one field")
	}
	if *c.AlertRepeatInterval <= 0 {
		return fmt.Errorf("AlertRepeatInterval must be > 0, got %v", *c.AlertRepeatInterval)
	}
	return nil
}
