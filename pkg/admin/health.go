package admin

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/agentstax/vulkan/pkg/metrics"
	"github.com/agentstax/vulkan/pkg/topic"
)

type VersionHealth struct {
	Topic     *topic.Topic
	Compacted bool
	Groups    []metrics.ConsumerGroupSnapshot
	Safe      bool
	Reason    string
}

// FamilyHealth is every topic's (name, version) registered, each with its own
// retire verdict -- the decision is per version, not per family.
func (a *MessageAdmin) FamilyHealth(ctx context.Context, name string) ([]*VersionHealth, error) {
	if name == "" {
		return nil, errors.New("topic name is required")
	}

	all, err := a.topicController.List(ctx)
	if err != nil {
		return nil, err
	}

	// ListTopics is already ORDER BY name, schema_version -- a name's versions
	// come out in order, so no separate query or re-sort is needed here.
	health := make([]*VersionHealth, 0, len(all))
	for _, listed := range all {
		if listed.Name != name {
			continue
		}
		h, err := a.versionHealth(ctx, listed)
		if err != nil {
			return nil, err
		}
		health = append(health, h)
	}
	return health, nil
}

func (a *MessageAdmin) versionHealth(ctx context.Context, found *topic.Topic) (*VersionHealth, error) {
	snapshot, err := a.metricsController.TopicSnapshot(ctx, found.Id)
	if err != nil {
		return nil, err
	}

	h := &VersionHealth{Topic: found, Compacted: snapshot.Compacted, Groups: snapshot.Groups}
	h.evaluate()

	return h, nil
}

// evaluate settles Safe/Reason from Compacted and Groups. Compacted always
// wins -- retention never reclaims a compacted key's winner, so no amount of
// group drain makes destroying it safe; that requires the bridge pattern
// (user-space, re-produce into the new version) instead.
func (h *VersionHealth) evaluate() {
	if h.Compacted {
		h.Reason = "compacted: requires bridge, never safe to retire on lag alone"
		return
	}
	if len(h.Groups) == 0 {
		h.Safe = true
		h.Reason = "safe: no consumer group has ever registered a cursor against this version"
		return
	}

	var lagging []string
	for _, group := range h.Groups {
		lag := group.GroupLag()
		if lag.Lag > 0 || lag.UnresolvedExceptions > 0 {
			lagging = append(lagging, group.ConsumerGroup)
		}
	}
	if len(lagging) == 0 {
		h.Safe = true
		h.Reason = "safe: every bound group has drained"
		return
	}
	h.Reason = fmt.Sprintf("not drained: %s", strings.Join(lagging, ", "))
}
