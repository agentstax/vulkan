package controller

import (
	"context"
	"errors"
	"time"

	"github.com/agentstax/vulkan/pkg/alert"
	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/produce"
)

// Record classifies found against the owner's compaction head and produces
// the outcome. found is nil when the run found nothing -- it still flows
// to classify so an active head resolves.
func (c *AlertController) Record(ctx context.Context, name string, owner *common.Owner, found *alert.Alert) (alert.RecordOutcome, error) {
	if owner == nil {
		return "", errors.New("owner must not be nil")
	}

	messageKey, err := alert.MessageKey(name, owner)
	if err != nil {
		return "", err
	}

	head, err := c.heads.GetHead[alert.Alert](ctx, c.alerts.Topic.Id, messageKey)
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

	compaction, err := produce.NewCompactionOptions(0)
	if err != nil {
		return "", err
	}
	if _, err := c.alerts.Produce(ctx, published, &produce.ProduceOptions{
		RoutingKey: published.RoutingKey(),
		MessageKey: messageKey,
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
		c.Logger.InfoContext(ctx, "alert resolved",
			"alert", published.Name, "alert_message", published.Message, "owner", published.Owner.Name)
		return
	}
	c.Logger.WarnContext(ctx, "alert active",
		"alert", published.Name, "alert_message", published.Message,
		"detail", published.Detail, "hint", published.Hint,
		"owner", published.Owner.Name, "severity", published.Severity)
}
