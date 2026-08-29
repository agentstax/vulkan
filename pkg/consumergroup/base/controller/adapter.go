package controller

import (
	"github.com/agentstax/vulkan/pkg/consumergroup/base/controller/datastore"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

func toKeyLeaseClaim(data *datastore.KeyLeaseData) *KeyLeaseClaim {
	return &KeyLeaseClaim{
		Verdict:         KeyLeaseVerdict(data.Verdict),
		TopicId:         data.TopicId,
		ConsumerGroupId: data.ConsumerGroupId,
		MessageKey:      data.MessageKey,
		Token:           uuid.UUID(data.Token.Bytes),
	}
}

func toKeyLeaseData(claim *KeyLeaseClaim) *datastore.KeyLeaseData {
	return &datastore.KeyLeaseData{
		Verdict:         datastore.KeyLeaseVerdict(claim.Verdict),
		TopicId:         claim.TopicId,
		ConsumerGroupId: claim.ConsumerGroupId,
		MessageKey:      claim.MessageKey,
		Token:           toTokenData(claim.Token),
	}
}

func toTokenData(token uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: token, Valid: true}
}
