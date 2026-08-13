package admin

import (
	"context"
	"errors"
	"fmt"

	consumercontroller "github.com/agentstax/vulkan/pkg/consumer/controller"
	"github.com/agentstax/vulkan/pkg/topic"
)

// DestroyGroup permanently deletes the consumer group registered under
// groupName on topic (topicName, version): its cursor, bindings, leases,
// delivery rows, group-owned workers, and group-owned cron jobs. The
// topic and its messages are untouched.
//
// Returns ErrDestroyDisabled unless MessageAdminConfig.AllowDestroy is set,
// and ErrTopicNotFound / ErrGroupNotFound when either side is missing.
// Unless opts.Force is set:
//   - a consumer still runs on the group     -> ErrGroupLive
//   - the group still holds delivery rows    -> ErrGroupDeliveriesPending
func (a *MessageAdmin) DestroyGroup(ctx context.Context, topicName string, version topic.SchemaVersion, groupName string, opts DestroyOptions) error {
	if !a.allowDestroy {
		return ErrDestroyDisabled
	}
	if topicName == "" {
		return errors.New("topic name is required")
	}
	if groupName == "" {
		return errors.New("group name is required")
	}

	found, err := a.topicController.GetTopic(ctx, topicName, version)
	if err != nil {
		return err
	}
	if found == nil {
		return fmt.Errorf("%w: %s version %d", topic.ErrTopicNotFound, topicName, version)
	}

	group, err := a.consumerController.GetGroup(ctx, found.Id, groupName)
	if err != nil {
		return err
	}
	if group == nil {
		return fmt.Errorf("%w: %s on topic %s", consumercontroller.ErrGroupNotFound, groupName, topicName)
	}

	if !opts.Force {
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
			return fmt.Errorf("%w: %s", consumercontroller.ErrGroupLive, groupName)
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
		return fmt.Errorf("%w: %s", consumercontroller.ErrGroupDeliveriesPending, groupName)
	}
	return nil
}
