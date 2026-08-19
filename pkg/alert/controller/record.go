package controller

import (
	"context"
	"errors"
	"time"

	"github.com/agentstax/vulkan/pkg/alert"
	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/producer"
)

// Record classifies found against the owner's compaction head and produces
// the outcome. found is nil when the run found nothing -- it still flows
// to classify so an active head resolves.
func (c *AlertController) Record(ctx context.Context, name string, owner *common.Owner, found *alert.Alert) (alert.RecordOutcome, error) {
	if owner == nil {
		return "", errors.New("owner must not be nil")
	}

	compactionKey, err := alert.CompactionKey(name, owner)
	if err != nil {
		return "", err
	}

	head, err := c.heads.GetHead(ctx, c.alerts.Topic.Id, compactionKey)
	if err != nil {
		return "", err
	}

	published, err := classify(found, head, c.repeat, time.Now())
	if err != nil {
		return "", err
	}
	if published == nil {
		return alert.RecordOutcomeNothing, nil
	}

	compaction, err := producer.NewCompactionOptions(compactionKey, 0)
	if err != nil {
		return "", err
	}
	if _, err := c.alerts.Produce(ctx, published, producer.ProduceOptions{
		RoutingKey: published.RoutingKey(),
		Compaction: compaction,
	}); err != nil {
		return "", err
	}

	if statusChanged(published, head) {
		c.logAlerts(ctx, published)
	}
	if published.Status == alert.StatusResolved {
		return alert.RecordOutcomeResolved, nil
	}
	return alert.RecordOutcomeActive, nil
}

func (c *AlertController) logAlerts(ctx context.Context, published *alert.Alert) {
	if published.Status == alert.StatusResolved {
		c.Logger.InfoContext(ctx, published.Message,
			"alert", published.Name, "owner", published.Owner.Name)
		return
	}
	c.Logger.WarnContext(ctx, published.Message,
		"detail", published.Detail, "hint", published.Hint,
		"alert", published.Name, "owner", published.Owner.Name, "severity", published.Severity)
}
