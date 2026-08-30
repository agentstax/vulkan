package controller

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/agentstax/vulkan/pkg/producer/controller/datastore"
	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/google/uuid"
)

// idempotencyKeyNamespace is the UUIDv5 namespace a non-UUID IdempotencyKey
// string hashes under. Frozen: changing it would change every derived key.
var idempotencyKeyNamespace = uuid.MustParse("b6f6c193-4e2b-45c1-9d3f-6a821e0c7a54")

// ProduceFunc runs inside the append's transaction; the type and its docs
// live with the datastore.
type ProduceFunc[Message topic.Versioned] = datastore.ProduceFunc[Message]

// Append is one message to append: the same input AppendMessage takes as
// separate params.
type Append[Message topic.Versioned] struct {
	Payload *Message
	Options ProduceOptions
}

func NewAppend[Message topic.Versioned](payload *Message, options ProduceOptions) (*Append[Message], error) {
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
func (c *ProducerController) AppendMessage[Message topic.Versioned](ctx context.Context, topicId int64, partitionSize int64, produceFunc ProduceFunc[Message], options ProduceOptions) (*Appended[Message], error) {
	if topicId <= 0 {
		return nil, fmt.Errorf("topicId must be > 0, got %d", topicId)
	}
	if produceFunc == nil {
		return nil, errors.New("produceFunc must not be nil")
	}
	if err := options.Validate(); err != nil {
		return nil, err
	}

	idempotencyKey, err := resolveIdempotencyKey(options.IdempotencyKey)
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
func (c *ProducerController) AppendMessageInTx[Message topic.Versioned](ctx context.Context, tx Tx, topicId int64, partitionSize int64, produceFunc ProduceFunc[Message], options ProduceOptions) (*Appended[Message], error) {
	if tx == nil {
		return nil, errors.New("tx must not be nil")
	}
	if topicId <= 0 {
		return nil, fmt.Errorf("topicId must be > 0, got %d", topicId)
	}
	if produceFunc == nil {
		return nil, errors.New("produceFunc must not be nil")
	}
	if err := options.Validate(); err != nil {
		return nil, err
	}

	idempotencyKey, err := resolveIdempotencyKey(options.IdempotencyKey)
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
func (c *ProducerController) AppendMessageBatch[Message topic.Versioned](ctx context.Context, topicId int64, partitionSize int64, attemptTimeout time.Duration, appends []*Append[Message]) ([]Appended[Message], int, error) {
	if topicId <= 0 {
		return nil, -1, fmt.Errorf("topicId must be > 0, got %d", topicId)
	}
	if len(appends) == 0 {
		return nil, -1, errors.New("appends must not be empty")
	}

	appendData := make([]*datastore.AppendData[Message], 0, len(appends))
	for _, item := range appends {
		// cant batch produce a message without an IdempotencyKey
		// a rerun dedups an ambiguous commit only by reusing the IdempotencyKey
		if item.Options.IdempotencyKey == "" {
			return nil, -1, errors.New("append Options.IdempotencyKey is required")
		}
		resolved, err := resolveIdempotencyKey(item.Options.IdempotencyKey)
		if err != nil {
			return nil, -1, err
		}
		appendData = append(appendData, toAppendData(resolved, item.Payload, item.Options))
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

// ***************
// *** HELPERS ***
// ***************

// resolveIdempotencyKey resolves the caller's key to the UUID the claim
// table stores: "" mints a fresh UUIDv7, a string that parses as a UUID is
// used verbatim, anything else hashes to a deterministic UUIDv5.
func resolveIdempotencyKey(key string) (uuid.UUID, error) {
	if key == "" {
		return uuid.NewV7()
	}
	if parsed, err := uuid.Parse(key); err == nil {
		return parsed, nil
	}
	return uuid.NewSHA1(idempotencyKeyNamespace, []byte(key)), nil
}
