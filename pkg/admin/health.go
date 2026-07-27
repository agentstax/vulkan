package admin

import (
	"context"
	"errors"
	"fmt"
	"strings"

	metricsDatastore "github.com/agentstax/vulkan/pkg/metrics/datastore"
	"github.com/agentstax/vulkan/pkg/topic"
)

// VersionHealth is one registered topic's (name, version) consumption picture
type VersionHealth struct {
	Topic     *topic.Topic
	Compacted bool
	Groups    []metricsDatastore.GroupLag
	Safe      bool
	Reason    string
}

// FamilyHealth is every topic's (name, version) registered, each with its own
// retire verdict -- the decision is per version, not per family.
func (a *MessageAdmin) FamilyHealth(ctx context.Context, name string) ([]*VersionHealth, error) {
	if name == "" {
		return nil, errors.New("topic name is required")
	}

	all, err := a.topicDatastore.ListTopics(ctx)
	if err != nil {
		return nil, err
	}

	// ListTopics is already ORDER BY name, schema_version -- a name's versions
	// come out in order, so no separate query or re-sort is needed here.
	health := make([]*VersionHealth, 0, len(all))
	for _, t := range all {
		if t.Name != name {
			continue
		}
		h, err := a.versionHealth(ctx, t)
		if err != nil {
			return nil, err
		}
		health = append(health, h)
	}
	return health, nil
}

func (a *MessageAdmin) versionHealth(ctx context.Context, t *topic.Topic) (*VersionHealth, error) {
	compacted, err := a.metricsDatastore.IsCompacted(ctx, t.Id)
	if err != nil {
		return nil, err
	}

	groupNames, err := a.metricsDatastore.ListConsumerGroups(ctx, t.Id)
	if err != nil {
		return nil, err
	}

	groups := make([]metricsDatastore.GroupLag, 0, len(groupNames))
	for _, group := range groupNames {
		snapshot, err := a.metricsDatastore.ConsumerGroupSnapshot(ctx, t.Id, group)
		if err != nil {
			return nil, err
		}
		groups = append(groups, snapshot.GroupLag())
	}

	h := &VersionHealth{Topic: t, Compacted: compacted, Groups: groups}
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
	for _, g := range h.Groups {
		if g.Lag > 0 || g.ParkedExceptions > 0 {
			lagging = append(lagging, g.ConsumerGroup)
		}
	}
	if len(lagging) == 0 {
		h.Safe = true
		h.Reason = "safe: every bound group has drained"
		return
	}
	h.Reason = fmt.Sprintf("not drained: %s", strings.Join(lagging, ", "))
}
