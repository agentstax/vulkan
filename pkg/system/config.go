package system

import (
	"errors"
	"fmt"
	"time"
)

// Config is control-plane settings scoped to no single topic.
type Config struct {
	// AdvisorPollRate - how often the advisor duty runs its structural checks
	// (partition-count ceiling, compaction read cost).
	// Default: 2 * time.Minute.
	AdvisorPollRate time.Duration

	// AdvisoryRepeatInterval - how long a firing advisory stays quiet before the
	// advisor re-emits it as a reminder.
	// Default: 4 * time.Hour (Alertmanager's default).
	AdvisoryRepeatInterval time.Duration
}

func (c *Config) WithDefaults() *Config {
	if c.AdvisorPollRate == 0 {
		c.AdvisorPollRate = 2 * time.Minute
	}
	if c.AdvisoryRepeatInterval == 0 {
		c.AdvisoryRepeatInterval = 4 * time.Hour
	}
	return c
}

// Validate runs after WithDefaults -- anything still out of range here was set
// by the caller, not left unset.
func (c *Config) Validate() error {
	if c.AdvisorPollRate < 0 {
		return fmt.Errorf("AdvisorPollRate must be >= 0, got %v", c.AdvisorPollRate)
	}
	if c.AdvisoryRepeatInterval < 0 {
		return fmt.Errorf("AdvisoryRepeatInterval must be >= 0, got %v", c.AdvisoryRepeatInterval)
	}
	return nil
}

// AlterConfig is AlterSystem's sparse patch -- a nil field means leave unchanged.
type AlterConfig struct {
	AdvisorPollRate        *time.Duration
	AdvisoryRepeatInterval *time.Duration
}

func (c *AlterConfig) Validate() error {
	if c.AdvisorPollRate == nil && c.AdvisoryRepeatInterval == nil {
		return errors.New("no fields set -- an alter must change at least one field")
	}
	if c.AdvisorPollRate != nil && *c.AdvisorPollRate <= 0 {
		return fmt.Errorf("AdvisorPollRate must be > 0, got %v", *c.AdvisorPollRate)
	}
	if c.AdvisoryRepeatInterval != nil && *c.AdvisoryRepeatInterval <= 0 {
		return fmt.Errorf("AdvisoryRepeatInterval must be > 0, got %v", *c.AdvisoryRepeatInterval)
	}
	return nil
}

// ToSystem builds the System cfg describes, with the given timestamps.
func (c *Config) ToSystem(createdAt, updatedAt time.Time) *System {
	s, _ := NewSystem(c.AdvisorPollRate, c.AdvisoryRepeatInterval, createdAt, updatedAt)
	return s
}
