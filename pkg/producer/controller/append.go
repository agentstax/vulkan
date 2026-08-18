package controller

import (
	"context"
	"errors"
	"time"

	"github.com/agentstax/vulkan/pkg/producer/controller/datastore"
	"github.com/google/uuid"
)

// ProduceFunc runs inside the append's transaction; the type and its docs
// live with the datastore.
type ProduceFunc[Message any] = datastore.ProduceFunc[Message]

// Append is one message to append: the same input AppendMessage takes as
// separate params.
type Append[Message any] struct {
	Payload *Message
	Options ProduceOptions
}

func NewAppend[Message any](payload *Message, options ProduceOptions) (*Append[Message], error) {
	if err := options.Validate(); err != nil {
		return nil, err
	}
	return &Append[Message]{
		Payload: payload,
		Options: options,
	}, nil
}

// AppendMessage appends one message in its own transaction, returning once it
// is durably committed: produceFunc runs inside it and returns the payload to
// store.
func (c *ProducerController[Message]) AppendMessage(ctx context.Context, topicId int64, partitionSize int64, produceFunc ProduceFunc[Message], options ProduceOptions) (*Appended[Message], error) {
	if topicId <= 0 {
		return nil, errors.New("topicId must be > 0")
	}
	if produceFunc == nil {
		return nil, errors.New("produceFunc must not be nil")
	}
	if err := options.Validate(); err != nil {
		return nil, err
	}

	idempotencyKey, err := resolveIdempotencyKey(options)
	if err != nil {
		return nil, err
	}

	appended, err := c.datastore.AppendMessage(ctx, topicId, partitionSize, produceFunc, toAppendData[Message](idempotencyKey, nil, options))
	if err != nil || appended == nil {
		return nil, err
	}
	return toAppended(appended), nil
}

// AppendMessageInTx appends produceFunc's message inside a transaction the
// caller owns -- it commits or rolls back with everything else in tx.
func (c *ProducerController[Message]) AppendMessageInTx(ctx context.Context, tx Tx, topicId int64, partitionSize int64, produceFunc ProduceFunc[Message], options ProduceOptions) (*Appended[Message], error) {
	if tx == nil {
		return nil, errors.New("tx must not be nil")
	}
	if topicId <= 0 {
		return nil, errors.New("topicId must be > 0")
	}
	if produceFunc == nil {
		return nil, errors.New("produceFunc must not be nil")
	}
	if err := options.Validate(); err != nil {
		return nil, err
	}

	idempotencyKey, err := resolveIdempotencyKey(options)
	if err != nil {
		return nil, err
	}

	appended, err := c.datastore.AppendMessageInTx(ctx, tx, topicId, partitionSize, produceFunc, toAppendData[Message](idempotencyKey, nil, options))
	if err != nil || appended == nil {
		return nil, err
	}
	return toAppended(appended), nil
}

// AppendMessageBatch commits every append in one transaction. failedIdx is
// the FIRST failure in pipeline order, -1 when the failure carries no index.
func (c *ProducerController[Message]) AppendMessageBatch(ctx context.Context, topicId int64, partitionSize int64, attemptTimeout time.Duration, appends []*Append[Message]) ([]Appended[Message], int, error) {
	if topicId <= 0 {
		return nil, -1, errors.New("topicId must be > 0")
	}
	if len(appends) == 0 {
		return nil, -1, errors.New("appends must not be empty")
	}

	appendData := make([]*datastore.AppendData[Message], 0, len(appends))
	for _, item := range appends {
		// cant batch produce a message without an IdempotencyKey
		// a rerun dedups an ambiguous commit only by reusing the IdempotencyKey
		if item.Options.IdempotencyKey == uuid.Nil {
			return nil, -1, errors.New("append Options.IdempotencyKey is required")
		}
		appendData = append(appendData, toAppendData(item.Options.IdempotencyKey, item.Payload, item.Options))
	}

	appendedData, failedIdx, err := c.datastore.AppendMessageBatch(ctx, topicId, partitionSize, attemptTimeout, appendData)
	if err != nil {
		return nil, failedIdx, err
	}

	appended := make([]Appended[Message], 0, len(appendedData))
	for _, data := range appendedData {
		appended = append(appended, *toAppended(&data))
	}
	return appended, -1, nil
}

// resolveIdempotencyKey generates a fresh UUIDv7 unless the caller supplied
// one
func resolveIdempotencyKey(options ProduceOptions) (uuid.UUID, error) {
	if options.IdempotencyKey != uuid.Nil {
		return options.IdempotencyKey, nil
	}
	return uuid.NewV7()
}
