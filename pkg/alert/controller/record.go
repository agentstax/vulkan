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
func (c *AlertController) Record(ctx context.Context, name string, owner *common.Owner, found *alert.Alert) error {
	if owner == nil {
		return errors.New("owner must not be nil")
	}

	compactionKey, err := alert.CompactionKey(name, owner)
	if err != nil {
		return err
	}

	head, err := c.heads.GetCompactionHead(ctx, c.alerts.Topic.Id, compactionKey)
	if err != nil {
		return err
	}

	published, err := classify(found, head, c.repeat, time.Now())
	if err != nil {
		return err
	}
	if published == nil {
		return nil
	}

	if _, err := c.alerts.Produce(ctx, published, producer.ProduceOptions{
		RoutingKey:    published.RoutingKey(),
		CompactionKey: compactionKey,
	}); err != nil {
		return err
	}

	if statusChanged(published, head) {
		c.logAlerts(ctx, published)
	}
	return nil
}

func (c *AlertController) logAlerts(ctx context.Context, published *alert.Alert) {
	if published.Status == alert.StatusResolved {
		c.logger.InfoContext(ctx, published.Message,
			"alert", published.Name, "owner", published.Owner.Name)
		return
	}
	c.logger.WarnContext(ctx, published.Message,
		"detail", published.Detail, "hint", published.Hint,
		"alert", published.Name, "owner", published.Owner.Name, "severity", published.Severity)
}
