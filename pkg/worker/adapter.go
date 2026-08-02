package worker

import (
	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/worker/datastore"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

func toWorker(data datastore.ListWorkersData) (*Worker, error) {
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

	return &Worker{
		Id:              data.Id,
		Name:            data.Name,
		Owner:           *owner,
		Metadata:        data.Metadata,
		TargetInstances: data.TargetInstances,
	}, nil
}

func toWorkerData(w *Worker) *datastore.WorkerData {
	return &datastore.WorkerData{
		Id:              w.Id,
		SystemId:        w.Owner.SystemIdColumn(),
		TopicId:         w.Owner.TopicIdColumn(),
		ConsumerGroupId: w.Owner.ConsumerGroupIdColumn(),
		Name:            w.Name,
		Metadata:        w.Metadata,
		TargetInstances: w.TargetInstances,
	}
}

func toWorkerInstance(data *datastore.WorkerInstanceData) *WorkerInstance {
	return &WorkerInstance{
		Id:        data.Id,
		WorkerId:  data.WorkerId,
		Token:     uuid.UUID(data.Token.Bytes),
		ExpiresAt: data.ExpiresAt,
		Attempts:  data.Attempts,
	}
}

func toWorkerInstanceData(instance *WorkerInstance) *datastore.WorkerInstanceData {
	return &datastore.WorkerInstanceData{
		Id:        instance.Id,
		WorkerId:  instance.WorkerId,
		Token:     pgtype.UUID{Bytes: instance.Token, Valid: true},
		ExpiresAt: instance.ExpiresAt,
		Attempts:  instance.Attempts,
	}
}
