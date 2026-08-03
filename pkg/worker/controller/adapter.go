package controller

import (
	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/worker"
	"github.com/agentstax/vulkan/pkg/worker/controller/datastore"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

func toWorker(data datastore.ListWorkersData) (*worker.Worker, error) {
	var owner *common.Owner
	var err error
	switch {
	case data.ConsumerGroupId != nil:
		owner, err = common.NewConsumerGroupOwner(data.OwnerSystemId, data.OwnerTopicId, *data.ConsumerGroupId, data.ConsumerGroup)
	case data.TopicId != nil:
		owner, err = common.NewTopicOwner(data.OwnerSystemId, *data.TopicId, data.TopicName)
	default:
		owner, err = common.NewSystemOwner(data.OwnerSystemId)
	}
	if err != nil {
		return nil, err
	}

	return &worker.Worker{
		Id:              data.Id,
		Name:            data.Name,
		Owner:           *owner,
		Metadata:        data.Metadata,
		TargetInstances: data.TargetInstances,
	}, nil
}

func toWorkerInstance(data *datastore.WorkerInstanceData) *worker.WorkerInstance {
	return &worker.WorkerInstance{
		Id:        data.Id,
		WorkerId:  data.WorkerId,
		Token:     uuid.UUID(data.Token.Bytes),
		ExpiresAt: data.ExpiresAt,
		Attempts:  data.Attempts,
	}
}

func toTokenData(token uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: token, Valid: true}
}
