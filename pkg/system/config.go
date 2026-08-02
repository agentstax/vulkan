package system

import (
	"errors"
	"fmt"
	"time"
)

// Config is control-plane settings scoped to no single topic.
type Config struct {
	// AlertRepeatInterval - how long a firing alert stays quiet before it
	// re-emits as a reminder. Default 4h.
	AlertRepeatInterval time.Duration
}

func (c *Config) WithDefaults() *Config {
	if c.AlertRepeatInterval == 0 {
		c.AlertRepeatInterval = 4 * time.Hour
	}
	return c
}

// Validate runs after WithDefaults -- anything still out of range here was set
// by the caller, not left unset.
func (c *Config) Validate() error {
	if c.AlertRepeatInterval < 0 {
		return fmt.Errorf("AlertRepeatInterval must be >= 0, got %v", c.AlertRepeatInterval)
	}
	return nil
}

// AlterConfig is AlterSystem's sparse patch -- a nil field means leave unchanged.
type AlterConfig struct {
	AlertRepeatInterval *time.Duration
}

func (c *AlterConfig) Validate() error {
	if c.AlertRepeatInterval == nil {
		return errors.New("no fields set -- an alter must change at least one field")
	}
	if *c.AlertRepeatInterval <= 0 {
		return fmt.Errorf("AlertRepeatInterval must be > 0, got %v", *c.AlertRepeatInterval)
	}
	return nil
}

// ToSystem builds the System cfg describes, with the given identity and timestamps.
func (c *Config) ToSystem(id int64, createdAt, updatedAt time.Time) *System {
	s, _ := NewSystem(id, c.AlertRepeatInterval, createdAt, updatedAt)
	return s
}
