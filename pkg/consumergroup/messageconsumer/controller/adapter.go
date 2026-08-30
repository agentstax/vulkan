package controller

import (
	"github.com/agentstax/vulkan/pkg/consumergroup/messageconsumer/controller/datastore"
	"github.com/jackc/pgx/v5/pgtype"
	"uuid"
)

func toMessage(data datastore.MessageData) Message {
	return Message{
		Id:             data.Id,
		Payload:        data.Payload,
		CreatedAt:      data.CreatedAt,
		RoutingKey:     data.RoutingKey,
		MessageKey:     data.MessageKey,
		CompactionRank: data.CompactionRank,
		Compacted:      data.Compacted,
		Options:        data.Options,
	}
}

func toRangeLease(data datastore.LeaseData) RangeLease {
	return RangeLease{
		Token:           uuid.UUID(data.Token.Bytes),
		ConsumerGroupId: data.ConsumerGroupId,
		Low:             data.Low,
		High:            data.High,
		ExpiresAt:       data.ExpiresAt,
		Reclaims:        data.Reclaims,
	}
}

func toClaimedRange(data *datastore.ClaimedRangeData) *ClaimedRange {
	messages := make([]Message, 0, len(data.Messages))
	for _, message := range data.Messages {
		messages = append(messages, toMessage(message))
	}
	return &ClaimedRange{
		Lease:       toRangeLease(data.Lease),
		Messages:    messages,
		Quarantined: data.Quarantined,
	}
}

func toOutcomeData(outcomes []MessageOutcome) []datastore.OutcomeData {
	data := make([]datastore.OutcomeData, 0, len(outcomes))
	for _, outcome := range outcomes {
		data = append(data, datastore.OutcomeData{
			MessageId:   outcome.MessageId,
			MessageKey:  outcome.MessageKey,
			Concurrency: outcome.Concurrency,
			Kind:        datastore.OutcomeKind(outcome.Kind),
			Err:         outcome.Err,
			Delay:       outcome.Delay,
		})
	}
	return data
}

func toTokenData(token uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: token, Valid: true}
}
