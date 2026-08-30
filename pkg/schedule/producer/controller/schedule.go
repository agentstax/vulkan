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

// DueSchedule is the locked row snapshot one producing transaction works from.
type DueSchedule struct {
	Id              int64
	Name            string
	Expression      string
	Concurrency     common.ConcurrencyPolicy
	Timeout         time.Duration
	Payload         json.RawMessage
	Metadata        json.RawMessage
	NextScheduledAt time.Time
	DbNow           time.Time
}

// ListDue returns the ids of every unsuspended row whose
// next_scheduled_at has passed, newest-due first.
func (c *ScheduleProducerController) ListDue(ctx context.Context) ([]int64, error) {
	return c.datastore.ListDue(ctx)
}

// ClaimDue rereads the row under the caller's transaction lock,
// making the unlocked due scan safe -- nil means it raced away (suspended,
// destroyed, or another scheduler's transaction holds it).
// Runs inside the produce transaction, on the caller's q.
func (c *ScheduleProducerController) ClaimDue(ctx context.Context, q iDatastore.Querier, id int64) (*DueSchedule, error) {
	if id <= 0 {
		return nil, fmt.Errorf("id must be > 0, got %d", id)
	}

	data, err := c.datastore.ClaimDue(ctx, q, id)
	if err != nil || data == nil {
		return nil, err
	}
	return toDueSchedule(data), nil
}

// Advance moves the produced row to its next scheduled time, in the
// caller's producing transaction.
func (c *ScheduleProducerController) Advance(ctx context.Context, q iDatastore.Querier, id int64, next time.Time, produced time.Time) error {
	if id <= 0 {
		return fmt.Errorf("id must be > 0, got %d", id)
	}
	if next.IsZero() {
		return errors.New("next must not be zero")
	}
	if produced.IsZero() {
		return errors.New("produced must not be zero")
	}

	return c.datastore.Advance(ctx, q, id, next, produced)
}

// Suspend sets the row suspended, in the caller's producing
// transaction -- next_scheduled_at is NOT NULL and an unsatisfiable
// schedule has no honest value for it.
func (c *ScheduleProducerController) Suspend(ctx context.Context, q iDatastore.Querier, id int64, produced time.Time) error {
	if id <= 0 {
		return fmt.Errorf("id must be > 0, got %d", id)
	}
	if produced.IsZero() {
		return errors.New("produced must not be zero")
	}

	return c.datastore.Suspend(ctx, q, id, produced)
}
