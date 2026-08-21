package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	keyleasecontroller "github.com/agentstax/vulkan/pkg/consumergroup/base/controller"
	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/google/uuid"
)

// one exception claimed off the exception window for (re)processing -- the
// lease token guards its resolution: every write against the row matches on it.
type ClaimedException struct {
	ConsumerGroupId int64
	TopicId         int64
	MessageId       int64
	Attempts        int
	LeaseToken      uuid.UUID
	LeaseUntil      time.Time
	Payload         json.RawMessage
	CreatedAt       time.Time
	RoutingKey      string
	CompactionKey   string
	CompactionRank  int64
	Options         *common.MessageOptions
}

// Kill marks expired 'inflight' rows that are out of attempts 'dead'
// so nothing else resolves them. Run it before Claim so an exhausted
// expired row is dead-lettered rather than claimed again.
// Returns how many rows it marked.
func (c *ExceptionConsumerGroupController) Kill(ctx context.Context, topicId int64, groupId int64, maxRetries int, deliveryLogMode topic.DeliveryLogMode) (int64, error) {
	if topicId <= 0 {
		return 0, fmt.Errorf("topicId must be > 0, got %d", topicId)
	}
	if groupId <= 0 {
		return 0, fmt.Errorf("groupId must be > 0, got %d", groupId)
	}
	if maxRetries < 0 {
		return 0, fmt.Errorf("maxRetries must be >= 0, got %d", maxRetries)
	}

	return c.datastore.Kill(ctx, topicId, groupId, maxRetries, deliveryLogMode)
}

// Claim claims 'ready', expired 'inflight', and 'deferred' rows up
// to maxRetries attempts. A leased compaction key excludes its rows.
func (c *ExceptionConsumerGroupController) Claim(ctx context.Context, topicId int64, groupId int64, limit int, maxRetries int, leaseDuration time.Duration, deliveryLogMode topic.DeliveryLogMode) ([]ClaimedException, error) {
	if topicId <= 0 {
		return nil, fmt.Errorf("topicId must be > 0, got %d", topicId)
	}
	if groupId <= 0 {
		return nil, fmt.Errorf("groupId must be > 0, got %d", groupId)
	}
	if limit < 1 {
		return nil, fmt.Errorf("limit must be >= 1, got %d", limit)
	}
	if maxRetries < 0 {
		return nil, fmt.Errorf("maxRetries must be >= 0, got %d", maxRetries)
	}
	if leaseDuration <= 0 {
		return nil, fmt.Errorf("leaseDuration must be > 0, got %v", leaseDuration)
	}

	claimed, err := c.datastore.Claim(ctx, topicId, groupId, limit, maxRetries, leaseDuration, deliveryLogMode)
	if err != nil {
		return nil, err
	}

	exceptions := make([]ClaimedException, 0, len(claimed))
	for _, data := range claimed {
		exceptions = append(exceptions, toClaimedException(data))
	}
	return exceptions, nil
}

// RenewLease extends a claim the caller already won.
// false -> the lease was taken over by another claim.
func (c *ExceptionConsumerGroupController) RenewLease(ctx context.Context, exception *ClaimedException, duration time.Duration) (bool, error) {
	if exception == nil {
		return false, errors.New("exception must not be nil")
	}
	if duration <= 0 {
		return false, fmt.Errorf("duration must be > 0, got %v", duration)
	}

	return c.datastore.RenewLease(ctx, toExceptionData(exception), duration)
}

// RecordSuccess deletes the row
// DeliveryLogModeAll also writes the 'success' log row in the same statement.
// A non-nil keyClaim frees the key in the same transaction.
func (c *ExceptionConsumerGroupController) RecordSuccess(ctx context.Context, exception *ClaimedException, deliveryLogMode topic.DeliveryLogMode, keyClaim *keyleasecontroller.KeyLeaseClaim) error {
	if exception == nil {
		return errors.New("exception must not be nil")
	}

	return c.datastore.RecordSuccess(ctx, toExceptionData(exception), deliveryLogMode, toKeyLeaseData(keyClaim))
}

// RecordFailure resets the row as 'ready' with retryPolicy's
// backoff so it can be retried.
// A non-nil keyClaim frees the key in the same transaction.
func (c *ExceptionConsumerGroupController) RecordFailure(ctx context.Context, retryPolicy *common.RetryPolicy, exception *ClaimedException, failureErr error, deliveryLogMode topic.DeliveryLogMode, keyClaim *keyleasecontroller.KeyLeaseClaim) error {
	if retryPolicy == nil {
		return errors.New("retryPolicy must not be nil")
	}
	if exception == nil {
		return errors.New("exception must not be nil")
	}
	if failureErr == nil {
		return errors.New("failureErr must not be nil")
	}

	return c.datastore.RecordFailure(ctx, retryPolicy, toExceptionData(exception), failureErr, deliveryLogMode, toKeyLeaseData(keyClaim))
}

// RecordTerminal marks the row 'dead' -- no retry could succeed.
// A non-nil keyClaim frees the key in the same transaction.
func (c *ExceptionConsumerGroupController) RecordTerminal(ctx context.Context, exception *ClaimedException, failureErr error, deliveryLogMode topic.DeliveryLogMode, keyClaim *keyleasecontroller.KeyLeaseClaim) error {
	if exception == nil {
		return errors.New("exception must not be nil")
	}
	if failureErr == nil {
		return errors.New("failureErr must not be nil")
	}

	return c.datastore.RecordTerminal(ctx, toExceptionData(exception), failureErr, deliveryLogMode, toKeyLeaseData(keyClaim))
}

// RecordSuperseded never runs the row again: the claim's attempts
// increment is decremented back and the log row lands at that attempt.
func (c *ExceptionConsumerGroupController) RecordSuperseded(ctx context.Context, exception *ClaimedException, deliveryLogMode topic.DeliveryLogMode) error {
	if exception == nil {
		return errors.New("exception must not be nil")
	}

	return c.datastore.RecordSuperseded(ctx, toExceptionData(exception), deliveryLogMode)
}
