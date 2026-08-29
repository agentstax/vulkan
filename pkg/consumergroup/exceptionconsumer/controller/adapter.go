package controller

import (
	keyleasecontroller "github.com/agentstax/vulkan/pkg/consumergroup/base/controller"
	"github.com/agentstax/vulkan/pkg/consumergroup/exceptionconsumer/controller/datastore"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

func toClaimedException(data datastore.ExceptionData) ClaimedException {
	return ClaimedException{
		ConsumerGroupId: data.ConsumerGroupId,
		TopicId:         data.TopicId,
		MessageId:       data.MessageId,
		Attempts:        data.Attempts,
		LeaseToken:      uuid.UUID(data.LeaseToken.Bytes),
		LeaseExpiresAt:  data.LeaseExpiresAt,
		Payload:         data.Payload,
		CreatedAt:       data.CreatedAt,
		RoutingKey:      data.RoutingKey,
		CompactionKey:   data.CompactionKey,
		CompactionRank:  data.CompactionRank,
		Options:         data.Options,
	}
}

func toExceptionData(exception *ClaimedException) *datastore.ExceptionData {
	return &datastore.ExceptionData{
		ConsumerGroupId: exception.ConsumerGroupId,
		TopicId:         exception.TopicId,
		MessageId:       exception.MessageId,
		Attempts:        exception.Attempts,
		LeaseToken:      toTokenData(exception.LeaseToken),
		LeaseExpiresAt:  exception.LeaseExpiresAt,
		Payload:         exception.Payload,
		CreatedAt:       exception.CreatedAt,
		RoutingKey:      exception.RoutingKey,
		CompactionKey:   exception.CompactionKey,
		CompactionRank:  exception.CompactionRank,
		Options:         exception.Options,
	}
}

// nil in, nil out -- the record verbs take an optional key claim.
func toKeyLeaseData(claim *keyleasecontroller.KeyLeaseClaim) *datastore.KeyLeaseData {
	if claim == nil {
		return nil
	}
	return &datastore.KeyLeaseData{
		TopicId:         claim.TopicId,
		ConsumerGroupId: claim.ConsumerGroupId,
		CompactionKey:   claim.CompactionKey,
		Token:           toTokenData(claim.Token),
	}
}

func toTokenData(token uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: token, Valid: true}
}
