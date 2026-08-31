package controller

import (
	keyleasecontroller "github.com/agentstax/vulkan/pkg/consumergroup/base/controller"
	"github.com/agentstax/vulkan/pkg/consumergroup/exceptionconsumer/controller/datastore"
	"github.com/jackc/pgx/v5/pgtype"
	"uuid"
)

func toClaimedException(data datastore.ExceptionQueueRow) ClaimedException {
	return ClaimedException{
		ConsumerGroupId: data.ConsumerGroupId,
		TopicId:         data.TopicId,
		MessageId:       data.MessageId,
		Attempts:        data.Attempts,
		Delays:          data.Delays,
		LeaseToken:      uuid.UUID(data.LeaseToken.Bytes),
		LeaseExpiresAt:  data.LeaseExpiresAt,
		Payload:         data.Payload,
		CreatedAt:       data.CreatedAt,
		RoutingKey:      data.RoutingKey,
		MessageKey:      data.MessageKey,
		CompactionRank:  data.CompactionRank,
		Compacted:       data.Compacted,
		Options:         data.Options,
	}
}

func toExceptionQueueRow(exception *ClaimedException) *datastore.ExceptionQueueRow {
	return &datastore.ExceptionQueueRow{
		ConsumerGroupId: exception.ConsumerGroupId,
		TopicId:         exception.TopicId,
		MessageId:       exception.MessageId,
		Attempts:        exception.Attempts,
		Delays:          exception.Delays,
		LeaseToken:      toTokenData(exception.LeaseToken),
		LeaseExpiresAt:  exception.LeaseExpiresAt,
		Payload:         exception.Payload,
		CreatedAt:       exception.CreatedAt,
		RoutingKey:      exception.RoutingKey,
		MessageKey:      exception.MessageKey,
		CompactionRank:  exception.CompactionRank,
		Compacted:       exception.Compacted,
		Options:         exception.Options,
	}
}

// nil in, nil out -- the record verbs take an optional key claim.
func toKeyLease(claim *keyleasecontroller.KeyLeaseClaim) *datastore.KeyLease {
	if claim == nil {
		return nil
	}
	return &datastore.KeyLease{
		TopicId:         claim.TopicId,
		ConsumerGroupId: claim.ConsumerGroupId,
		MessageKey:      claim.MessageKey,
		Token:           toTokenData(claim.Token),
	}
}

func toTokenData(token uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: token, Valid: true}
}
