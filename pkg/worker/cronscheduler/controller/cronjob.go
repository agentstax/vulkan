package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
)

// DueCronJob is the locked row snapshot one producing transaction works from.
type DueCronJob struct {
	Id                int64
	Name              string
	Schedule          string
	Concurrency       common.ConcurrencyPolicy
	Timeout           time.Duration
	Data              json.RawMessage
	Metadata          json.RawMessage
	NextScheduledTime time.Time
	DbNow             time.Time
}

// DueCronJobs returns the ids of every unsuspended row whose
// next_scheduled_time has passed, newest-due first.
func (c *CronSchedulerController) DueCronJobs(ctx context.Context) ([]int64, error) {
	return c.datastore.DueCronJobs(ctx)
}

// ClaimDueCronJob rereads the row under the caller's transaction lock,
// making the unlocked due scan safe -- nil means it raced away (suspended,
// destroyed, or another scheduler's transaction holds it).
// Runs inside the produce transaction, on the caller's q.
func (c *CronSchedulerController) ClaimDueCronJob(ctx context.Context, q iDatastore.Querier, id int64) (*DueCronJob, error) {
	if id <= 0 {
		return nil, fmt.Errorf("id must be > 0, got %d", id)
	}

	data, err := c.datastore.ClaimDueCronJob(ctx, q, id)
	if err != nil || data == nil {
		return nil, err
	}
	return toDueCronJob(data), nil
}

// AdvanceCronJob moves the produced row to its next scheduled time, in the
// caller's producing transaction.
func (c *CronSchedulerController) AdvanceCronJob(ctx context.Context, q iDatastore.Querier, id int64, next time.Time, produced time.Time) error {
	if id <= 0 {
		return fmt.Errorf("id must be > 0, got %d", id)
	}
	if next.IsZero() {
		return errors.New("next must not be zero")
	}
	if produced.IsZero() {
		return errors.New("produced must not be zero")
	}

	return c.datastore.AdvanceCronJob(ctx, q, id, next, produced)
}

// SuspendCronJob sets the row suspended, in the caller's producing
// transaction -- next_scheduled_time is NOT NULL and an unsatisfiable
// schedule has no honest value for it.
func (c *CronSchedulerController) SuspendCronJob(ctx context.Context, q iDatastore.Querier, id int64, produced time.Time) error {
	if id <= 0 {
		return fmt.Errorf("id must be > 0, got %d", id)
	}
	if produced.IsZero() {
		return errors.New("produced must not be zero")
	}

	return c.datastore.SuspendCronJob(ctx, q, id, produced)
}
