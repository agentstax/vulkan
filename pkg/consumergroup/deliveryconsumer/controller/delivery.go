package controller

// The LIFECYCLE path's controller half, ON HOLD -- see the deliveryconsumer
// package header for why and what would revive it.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/consumergroup/deliveryconsumer/controller/datastore"
	"github.com/agentstax/vulkan/pkg/topic"
)

// Delivery is one (consumer_group_id, message_id) row of the per-topic
// exception_queue table: the mutable per-consumer lifecycle state that lives off the
// immutable message_log. Payload is joined back in from message_log at claim
// time rather than stored on the row.
type Delivery struct {
	ConsumerGroupId int64
	TopicId         int64
	MessageId       int64
	Payload         json.RawMessage
	Status          datastore.DeliveryStatus
	Attempts        int
	Options         *common.MessageOptions
}

// FanOut materializes one delivery row per message this group is bound to
// receive. Scans only above the group's mark, so steady-state cost is O(new
// messages) per tick, not O(whole log).
func (c *DeliveryConsumerGroupController) FanOut(ctx context.Context, topicId int64, groupId int64, schemaVersion int64, limit int) error {
	if topicId <= 0 {
		return fmt.Errorf("topicId must be > 0, got %d", topicId)
	}
	if groupId <= 0 {
		return fmt.Errorf("groupId must be > 0, got %d", groupId)
	}
	if limit < 1 {
		return fmt.Errorf("limit must be >= 1, got %d", limit)
	}

	return c.datastore.FanOut(ctx, topicId, groupId, schemaVersion, limit)
}

// ClaimMessagesWithLifecycle moves this group's own 'ready' delivery rows to
// 'processing'. No lease: the lifecycle path never grew crash recovery.
func (c *DeliveryConsumerGroupController) ClaimMessagesWithLifecycle(ctx context.Context, topicId int64, groupId int64, limit int) ([]Delivery, error) {
	if topicId <= 0 {
		return nil, fmt.Errorf("topicId must be > 0, got %d", topicId)
	}
	if groupId <= 0 {
		return nil, fmt.Errorf("groupId must be > 0, got %d", groupId)
	}
	if limit < 1 {
		return nil, fmt.Errorf("limit must be >= 1, got %d", limit)
	}

	claimed, err := c.datastore.ClaimMessagesWithLifecycle(ctx, topicId, groupId, limit)
	if err != nil {
		return nil, err
	}

	deliveries := make([]Delivery, 0, len(claimed))
	for _, data := range claimed {
		deliveries = append(deliveries, toDelivery(data))
	}
	return deliveries, nil
}

// RecordSuccess marks a claimed delivery 'done'.
// DeliveryLogModeAll also writes the 'success' log row in the same statement.
// Terminal success for this (group, message); the message row is untouched
// and other groups are unaffected.
func (c *DeliveryConsumerGroupController) RecordSuccess(ctx context.Context, delivery *Delivery, deliveryLogMode topic.DeliveryLogMode) error {
	if delivery == nil {
		return errors.New("delivery must not be nil")
	}

	return c.datastore.RecordSuccess(ctx, toExceptionQueueRow(delivery), deliveryLogMode)
}

// RecordFailure retries until attempts are exhausted, then dead-letters. No
// retry backoff -- the exception_queue table carries no can_run_after, so a 'ready'
// row is simply re-claimed on the next poll.
func (c *DeliveryConsumerGroupController) RecordFailure(ctx context.Context, maxAttempts int, delivery *Delivery, failureErr error, deliveryLogMode topic.DeliveryLogMode) error {
	if delivery == nil {
		return errors.New("delivery must not be nil")
	}
	if failureErr == nil {
		return errors.New("failureErr must not be nil")
	}
	if maxAttempts < 0 {
		return fmt.Errorf("maxAttempts must be >= 0, got %d", maxAttempts)
	}

	return c.datastore.RecordFailure(ctx, maxAttempts, toExceptionQueueRow(delivery), failureErr, deliveryLogMode)
}

// RecordTerminal dead-letters a delivery: no more retries. One group can
// dead-letter a message while another processes the same offset fine.
func (c *DeliveryConsumerGroupController) RecordTerminal(ctx context.Context, delivery *Delivery, terminalErr error, deliveryLogMode topic.DeliveryLogMode) error {
	if delivery == nil {
		return errors.New("delivery must not be nil")
	}
	if terminalErr == nil {
		return errors.New("terminalErr must not be nil")
	}

	return c.datastore.RecordTerminal(ctx, toExceptionQueueRow(delivery), terminalErr, deliveryLogMode)
}
