package controller

import (
	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/worker"
	"github.com/agentstax/vulkan/pkg/worker/controller/datastore"
	"uuid"
)

func toWorkerData(data datastore.ListWorkersRow) (*worker.WorkerData, error) {
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

	return &worker.WorkerData{
		Id:              data.Id,
		Name:            data.Name,
		Owner:           owner,
		Metadata:        data.Metadata,
		TargetInstances: worker.InstanceTarget(data.TargetInstances),
	}, nil
}

// the owner was the lookup key here, so unlike toWorkerData there are no join
// columns to resolve it from
func toOwnedWorker(data *datastore.WorkerConfigRow, owner *common.Owner) *worker.WorkerData {
	return &worker.WorkerData{
		Id:              data.Id,
		Name:            data.Name,
		Owner:           owner,
		Metadata:        data.Metadata,
		TargetInstances: worker.InstanceTarget(data.TargetInstances),
	}
}

func toWorkerInstance(data *datastore.WorkerInstanceRow) *worker.WorkerInstance {
	return &worker.WorkerInstance{
		Id:       data.Id,
		WorkerId: data.WorkerId,
		Token:    uuid.UUID(data.Token.Bytes),
		Attempts: data.Attempts,
	}
}
