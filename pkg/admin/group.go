package admin

import (
	"context"
	"errors"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/consumergroup"
	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/agentstax/vulkan/pkg/worker"
)

// GetGroup reads the group's config.
// Returns ErrTopicNotFound / ErrGroupNotFound when either side is missing.
func (a *MessageAdmin) GetGroup(ctx context.Context, topicName string, version topic.SchemaVersion, groupName string) ([]*worker.Worker, error) {
	groupOwner, err := a.groupOwner(ctx, topicName, version, groupName)
	if err != nil {
		return nil, err
	}
	listed, err := a.workerController.ListWorkers(ctx, groupOwner)
	if err != nil {
		return nil, err
	}

	// ListWorkers walks the whole owner chain -- keep only the group's own rows
	var workers []*worker.Worker
	for _, row := range listed {
		if row.Owner.ConsumerGroupId == groupOwner.ConsumerGroupId {
			workers = append(workers, row)
		}
	}
	return workers, nil
}

// groupOwner resolves the group registered under groupName on topic
// (topicName, version) to its owner. Returns ErrTopicNotFound /
// ErrGroupNotFound when either side is missing.
func (a *MessageAdmin) groupOwner(ctx context.Context, topicName string, version topic.SchemaVersion, groupName string) (*common.Owner, error) {
	if topicName == "" {
		return nil, errors.New("topic name is required")
	}
	if groupName == "" {
		return nil, errors.New("group name is required")
	}

	found, err := a.topicController.Get(ctx, topicName, version)
	if err != nil {
		return nil, err
	}
	if found == nil {
		return nil, topic.ErrTopicNotFound.With("topic", topicName, "version", version)
	}

	group, err := a.consumerController.GetGroup(ctx, found.Id, groupName)
	if err != nil {
		return nil, err
	}
	if group == nil {
		return nil, consumergroup.ErrGroupNotFound.With("group", groupName, "topic", topicName)
	}

	return common.NewConsumerGroupOwner(found.SystemId, found.Id, group.Id, group.Name)
}

// DestroyGroup permanently deletes the consumer group registered under
// groupName on topic (topicName, version): its cursor, bindings, leases,
// delivery rows, group-owned workers, and group-owned cron jobs. The
// topic and its messages are untouched.
//
// Returns topic.ErrDestroyDisabled unless MessageAdminConfig.AllowDestroy is set,
// and ErrTopicNotFound / ErrGroupNotFound when either side is missing.
// Unless options.Force is set:
//   - a consumer still runs on the group     -> ErrGroupLive
//   - the group still holds delivery rows    -> ErrGroupDeliveriesPending
func (a *MessageAdmin) DestroyGroup(ctx context.Context, topicName string, version topic.SchemaVersion, groupName string, options DestroyOptions) error {
	if !a.allowDestroy {
		return topic.ErrDestroyDisabled
	}
	if topicName == "" {
		return errors.New("topic name is required")
	}
	if groupName == "" {
		return errors.New("group name is required")
	}

	found, err := a.topicController.Get(ctx, topicName, version)
	if err != nil {
		return err
	}
	if found == nil {
		return topic.ErrTopicNotFound.With("topic", topicName, "version", version)
	}

	group, err := a.consumerController.GetGroup(ctx, found.Id, groupName)
	if err != nil {
		return err
	}
	if group == nil {
		return consumergroup.ErrGroupNotFound.With("group", groupName, "topic", topicName)
	}

	if !options.Force {
		if err := a.assertGroupIdle(ctx, found.Id, group.Id, group.Name); err != nil {
			return err
		}
	}

	return a.consumerController.DeleteGroup(ctx, found.Id, group.Id, group.Name)
}

// assertGroupIdle is DestroyGroup's guard: nothing is consuming on the
// group and no delivery rows would be discarded. Both facts come from the
// metrics snapshots that already own them.
func (a *MessageAdmin) assertGroupIdle(ctx context.Context, topicId int64, groupId int64, groupName string) error {
	// a running consumer heartbeats its worker instances -- any live
	// instance on a group-owned worker means someone is consuming
	workers, err := a.metricsController.WorkerSnapshots(ctx)
	if err != nil {
		return err
	}
	for _, snapshot := range workers {
		if snapshot.Owner.ConsumerGroupId == groupId && snapshot.LiveInstances > 0 {
			return consumergroup.ErrGroupLive.With("group", groupName)
		}
	}

	// every delivery row is a failure that needs a retry or a dead-letter record
	group, err := a.metricsController.ConsumerGroupSnapshot(ctx, topicId, groupName)
	if err != nil {
		return err
	}
	exceptions := group.Exceptions
	total := exceptions.Ready + exceptions.Inflight + exceptions.Deferred + exceptions.Dead
	if total > 0 {
		return consumergroup.ErrGroupDeliveriesPending.With("group", groupName)
	}
	return nil
}
