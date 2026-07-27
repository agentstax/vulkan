package monitor

import (
	"context"

	metricsDatastore "github.com/agentstax/vulkan/pkg/metrics/datastore"
)

// TopicSnapshot is one topic's merged metrics picture -- compaction/group
// membership plus each bound group's live queue state and its
// abandoned/cleared event-derived numbers.
type TopicSnapshot struct {
	TopicID   int64
	Compacted bool
	Groups    []GroupSnapshot
}

// GroupSnapshot is one bound group's picture within a TopicSnapshot: the
// DB-snapshot queue state (live cursor/delivery/lease reads) alongside the
// __system.metrics event-derived abandoned-routine numbers for the same
// (topic, group).
type GroupSnapshot struct {
	ConsumerGroup string
	Queue         metricsDatastore.ConsumerGroupSnapshot
	Events        metricsDatastore.EventSnapshot
}

// TopicSnapshot runs every DB-snapshot + event-derived read for topicID as
// one cold pass -- works with a noop meter and nothing else running, same as
// every other Monitor read.
func (m *Monitor) TopicSnapshot(ctx context.Context, topicID int64) (*TopicSnapshot, error) {
	compacted, err := m.Datastore.IsCompacted(ctx, topicID)
	if err != nil {
		return nil, err
	}

	groupNames, err := m.Datastore.ListConsumerGroups(ctx, topicID)
	if err != nil {
		return nil, err
	}

	groups := make([]GroupSnapshot, 0, len(groupNames))
	for _, name := range groupNames {
		queue, err := m.Datastore.ConsumerGroupSnapshot(ctx, topicID, name)
		if err != nil {
			return nil, err
		}
		events, err := m.Datastore.EventSnapshot(ctx, topicID, name)
		if err != nil {
			return nil, err
		}
		groups = append(groups, GroupSnapshot{ConsumerGroup: name, Queue: *queue, Events: *events})
	}

	return &TopicSnapshot{TopicID: topicID, Compacted: compacted, Groups: groups}, nil
}
