package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	keyleasecontroller "github.com/agentstax/vulkan/pkg/consumer/base/controller"
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

// KillExceptions marks expired 'inflight' rows that are out of attempts 'dead'
// so nothing else resolves them. Run it before ClaimExceptions so an exhausted
// expired row is dead-lettered rather than claimed again.
func (c *ExceptionConsumerController) KillExceptions(ctx context.Context, topicId int64, groupId int64, maxRetries int, deliveryLogMode topic.DeliveryLogMode) error {
	if topicId <= 0 {
		return errors.New("topicId must be > 0")
	}
	if groupId <= 0 {
		return errors.New("groupId must be > 0")
	}
	if maxRetries < 0 {
		return fmt.Errorf("maxRetries must be >= 0, got %d", maxRetries)
	}

	return c.datastore.KillExceptions(ctx, topicId, groupId, maxRetries, deliveryLogMode)
}

// ClaimExceptions claims 'ready', expired 'inflight', and 'deferred' rows up
// to maxRetries attempts. A leased compaction key excludes its rows.
func (c *ExceptionConsumerController) ClaimExceptions(ctx context.Context, topicId int64, groupId int64, limit int, maxRetries int, leaseDuration time.Duration, deliveryLogMode topic.DeliveryLogMode) ([]ClaimedException, error) {
	if topicId <= 0 {
		return nil, errors.New("topicId must be > 0")
	}
	if groupId <= 0 {
		return nil, errors.New("groupId must be > 0")
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

	claimed, err := c.datastore.ClaimExceptions(ctx, topicId, groupId, limit, maxRetries, leaseDuration, deliveryLogMode)
	if err != nil {
		return nil, err
	}

	exceptions := make([]ClaimedException, 0, len(claimed))
	for _, data := range claimed {
		exceptions = append(exceptions, toClaimedException(data))
	}
	return exceptions, nil
}

// RenewExceptionLease extends a claim the caller already won.
// false -> the lease was taken over by another claim.
func (c *ExceptionConsumerController) RenewExceptionLease(ctx context.Context, exception *ClaimedException, duration time.Duration) (bool, error) {
	if exception == nil {
		return false, errors.New("exception must not be nil")
	}
	if duration <= 0 {
		return false, fmt.Errorf("duration must be > 0, got %v", duration)
	}

	return c.datastore.RenewExceptionLease(ctx, toExceptionData(exception), duration)
}

// RecordExceptionSuccess deletes the row
// DeliveryLogModeAll also writes the 'success' log row in the same statement.
// A non-nil keyClaim frees the key in the same transaction.
func (c *ExceptionConsumerController) RecordExceptionSuccess(ctx context.Context, exception *ClaimedException, deliveryLogMode topic.DeliveryLogMode, keyClaim *keyleasecontroller.KeyLeaseClaim) error {
	if exception == nil {
		return errors.New("exception must not be nil")
	}

	return c.datastore.RecordExceptionSuccess(ctx, toExceptionData(exception), deliveryLogMode, toKeyLeaseData(keyClaim))
}

// RecordExceptionFailure resets the row so it can be retried, or marks it
// 'dead' once retryPolicy's budget is spent.
// A non-nil keyClaim frees the key in the same transaction.
func (c *ExceptionConsumerController) RecordExceptionFailure(ctx context.Context, retryPolicy *common.RetryPolicy, exception *ClaimedException, failureErr error, deliveryLogMode topic.DeliveryLogMode, keyClaim *keyleasecontroller.KeyLeaseClaim) error {
	if retryPolicy == nil {
		return errors.New("retryPolicy must not be nil")
	}
	if exception == nil {
		return errors.New("exception must not be nil")
	}
	if failureErr == nil {
		return errors.New("failureErr must not be nil")
	}

	return c.datastore.RecordExceptionFailure(ctx, retryPolicy, toExceptionData(exception), failureErr, deliveryLogMode, toKeyLeaseData(keyClaim))
}

// RecordExceptionTerminal marks the row 'dead' -- no retry could succeed.
// A non-nil keyClaim frees the key in the same transaction.
func (c *ExceptionConsumerController) RecordExceptionTerminal(ctx context.Context, exception *ClaimedException, failureErr error, deliveryLogMode topic.DeliveryLogMode, keyClaim *keyleasecontroller.KeyLeaseClaim) error {
	if exception == nil {
		return errors.New("exception must not be nil")
	}
	if failureErr == nil {
		return errors.New("failureErr must not be nil")
	}

	return c.datastore.RecordExceptionTerminal(ctx, toExceptionData(exception), failureErr, deliveryLogMode, toKeyLeaseData(keyClaim))
}

// RecordExceptionSuperseded never runs the row again: the claim's attempts
// increment is decremented back and the log row lands at that attempt.
func (c *ExceptionConsumerController) RecordExceptionSuperseded(ctx context.Context, exception *ClaimedException, deliveryLogMode topic.DeliveryLogMode) error {
	if exception == nil {
		return errors.New("exception must not be nil")
	}

	return c.datastore.RecordExceptionSuperseded(ctx, toExceptionData(exception), deliveryLogMode)
}
