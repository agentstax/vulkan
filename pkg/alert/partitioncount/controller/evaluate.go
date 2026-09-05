package controller

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/agentstax/vulkan/pkg/alert"
	"github.com/agentstax/vulkan/pkg/common"
)

// warnDivisor halves the lock ceiling so the alert leaves headroom to act
// before Destroy starts failing.
const warnDivisor = 2

// Evaluate measures the owner's topic and returns its alert, nil when none
// applies. threshold 0 derives the live default: the lock ceiling / 2.
func (c *PartitionCountController) Evaluate(ctx context.Context, owner *common.Owner, threshold int64) (*alert.Alert, error) {
	if owner == nil {
		return nil, errors.New("owner must not be nil")
	}
	if threshold < 0 {
		return nil, fmt.Errorf("threshold must be >= 0, got %d", threshold)
	}

	ceiling, err := c.datastore.PartitionLockCeiling(ctx)
	if err != nil {
		return nil, err
	}
	if threshold == 0 {
		threshold = ceiling / warnDivisor
	}

	count, err := c.datastore.PartitionCount(ctx, owner.TopicId)
	if err != nil {
		return nil, err
	}
	if count < threshold {
		return nil, nil
	}
	return newPartitionCountAlert(owner, count, ceiling, threshold, time.Now())
}
