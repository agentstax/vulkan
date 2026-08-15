package admin

import (
	"errors"
	"fmt"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
)

// AlterGroupConfig is AlterGroup's patch -- each field updates the matching
// metadata key on the group's worker rows; the zero value leaves it alone.
type AlterGroupConfig struct {
	ClaimPollRate           common.Update[time.Duration]
	MaxRangeReclaims        common.Update[int]
	ExceptionInitialBackoff common.Update[time.Duration]
	Message                 common.Update[common.MessageOptions]
	ConcurrencyOverride     common.Update[common.ConcurrencyPolicy]
}

func (c *AlterGroupConfig) Validate() error {
	changed := false
	for _, update := range c.overrides() {
		if update.IsChanged() {
			changed = true
		}
	}
	if !changed {
		return errors.New("no fields set -- an alter must change at least one field")
	}
	if value, ok := c.ClaimPollRate.Value(); ok && value <= 0 {
		return fmt.Errorf("ClaimPollRate must be > 0, got %v", value)
	}
	if value, ok := c.MaxRangeReclaims.Value(); ok && value < 1 {
		return fmt.Errorf("MaxRangeReclaims must be >= 1, got %d", value)
	}
	if value, ok := c.ExceptionInitialBackoff.Value(); ok && value <= 0 {
		return fmt.Errorf("ExceptionInitialBackoff must be > 0, got %v", value)
	}
	if value, ok := c.Message.Value(); ok {
		if err := value.Validate(); err != nil {
			return fmt.Errorf("Message: %w", err)
		}
	}
	if value, ok := c.ConcurrencyOverride.Value(); ok {
		if err := value.Validate(); err != nil {
			return fmt.Errorf("ConcurrencyOverride: %w", err)
		}
	}
	return nil
}

// overrides is the config keyed by the consumer kinds' metadata JSON tags.
func (c *AlterGroupConfig) overrides() map[string]common.Update[any] {
	return map[string]common.Update[any]{
		"claim_poll_rate":           c.ClaimPollRate.Any(),
		"max_range_reclaims":        c.MaxRangeReclaims.Any(),
		"exception_initial_backoff": c.ExceptionInitialBackoff.Any(),
		"message":                   c.Message.Any(),
		"concurrency_override":      c.ConcurrencyOverride.Any(),
	}
}
