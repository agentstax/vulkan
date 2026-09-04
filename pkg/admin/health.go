package admin

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/agentstax/vulkan/pkg/metrics"
	"github.com/agentstax/vulkan/pkg/topic"
)

// TopicVersionHealth is one payload version's retire verdict on a topic: safe
// once no compaction head points at it and every group has read past it.
type TopicVersionHealth struct {
	Topic           *topic.Topic                            `json:"topic"`
	Version         int                                     `json:"version"`
	Messages        int64                                   `json:"messages"`
	CompactionHeads int64                                   `json:"compaction_heads"`
	Groups          []metrics.ConsumerGroupSchemaVersionLag `json:"groups"`
	Safe            bool                                    `json:"safe"`
	Reason          string                                  `json:"reason"`
}

// TopicHealth is every payload version present in the named topic's log,
// each with its own retire verdict. Returns ErrTopicNotFound if name isn't
// registered; an empty topic has no versions.
func (a *MessageAdmin) TopicHealth(ctx context.Context, name string) ([]*TopicVersionHealth, error) {
	if name == "" {
		return nil, errors.New("topic name is required")
	}

	found, err := a.topicController.Get(ctx, name)
	if err != nil {
		return nil, err
	}
	if found == nil {
		return nil, topic.ErrTopicNotFound.With("topic", name)
	}

	snapshots, err := a.metricsController.TopicSchemaVersionSnapshots(ctx, found.Id)
	if err != nil {
		return nil, err
	}

	health := make([]*TopicVersionHealth, 0, len(snapshots))
	for _, snapshot := range snapshots {
		h := &TopicVersionHealth{
			Topic:           found,
			Version:         snapshot.Version,
			Messages:        snapshot.Messages,
			CompactionHeads: snapshot.CompactionHeads,
			Groups:          snapshot.Groups,
		}
		h.evaluate()
		health = append(health, h)
	}
	return health, nil
}

// evaluate settles Safe/Reason. A compaction head at this version is live
// state no group drain removes -- the bridge must re-produce it first.
func (h *TopicVersionHealth) evaluate() {
	if h.CompactionHeads > 0 {
		h.Reason = fmt.Sprintf("compaction heads remain: %d keys still resolve to this version", h.CompactionHeads)
		return
	}

	var lagging []string
	for _, group := range h.Groups {
		if group.Unconsumed > 0 || group.UnresolvedExceptions > 0 {
			lagging = append(lagging, group.ConsumerGroup)
		}
	}
	if len(lagging) > 0 {
		h.Reason = fmt.Sprintf("not drained: %s", strings.Join(lagging, ", "))
		return
	}

	h.Safe = true
	h.Reason = "safe: no compaction head points at this version and every group has read past it"
}
