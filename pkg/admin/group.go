package admin

import (
	"context"
	"errors"
	"fmt"

	"github.com/agentstax/vulkan/pkg/common"
	consumercontroller "github.com/agentstax/vulkan/pkg/consumer/controller"
	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/agentstax/vulkan/pkg/worker"
	workercontroller "github.com/agentstax/vulkan/pkg/worker/controller"
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

// AlterGroup sets and unsets operator overrides on the group's config.
// Overrides survive redeploys until unset and take effect when the group
// next claims work, not live. A key the group's code doesn't declare fails
// the whole alter -- nothing changes.
func (a *MessageAdmin) AlterGroup(ctx context.Context, topicName string, version topic.SchemaVersion, groupName string, cfg *AlterGroupConfig) error {
	if cfg == nil {
		cfg = &AlterGroupConfig{}
	}
	if err := cfg.Validate(); err != nil {
		return err
	}

	groupOwner, err := a.groupOwner(ctx, topicName, version, groupName)
	if err != nil {
		return err
	}
	_, err = a.workerController.AlterWorkers(ctx, groupOwner, &workercontroller.AlterWorkerConfig{
		Overrides: cfg.overrides(),
	})
	return err
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

	found, err := a.topicController.GetTopic(ctx, topicName, version)
	if err != nil {
		return nil, err
	}
	if found == nil {
		return nil, fmt.Errorf("%w: %s version %d", topic.ErrTopicNotFound, topicName, version)
	}

	group, err := a.consumerController.GetGroup(ctx, found.Id, groupName)
	if err != nil {
		return nil, err
	}
	if group == nil {
		return nil, fmt.Errorf("%w: %s on topic %s", consumercontroller.ErrGroupNotFound, groupName, topicName)
	}

	return common.NewConsumerGroupOwner(found.SystemId, found.Id, group.Id, group.Name)
}

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
