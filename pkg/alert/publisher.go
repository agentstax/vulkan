package alert

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/producer"
)

// Publisher writes what a handler decided to the __system.alerts topic and
// logs status changes.
type Publisher struct {
	alerts *producer.ProducerInstance[Alert]
	repeat time.Duration
	logger logger.Logger
}

// alerts is a registered producer instance on the __system.alerts topic;
// repeat is the system row's AlertRepeatInterval.
func NewPublisher(alerts *producer.ProducerInstance[Alert], repeat time.Duration, log logger.Logger) (*Publisher, error) {
	if alerts == nil {
		return nil, errors.New("alert producer instance must not be nil")
	}
	if repeat <= 0 {
		return nil, fmt.Errorf("repeat must be > 0, got %v", repeat)
	}
	// an active head must repeat before retention sweeps it -- checked against
	// the registration default; a live topic altered below repeat is out of scope
	if retention := TopicConfig().RetentionTTL; repeat >= retention {
		return nil, fmt.Errorf("repeat must be < the %s topic's retention %v, got %v", TopicName, retention, repeat)
	}
	if log == nil {
		log = logger.NewDefaultLogger(os.Stdout)
	}
	return &Publisher{alerts: alerts, repeat: repeat, logger: log}, nil
}

// Publish classifies alert against the owner's compaction head and produces
// the outcome. alert is nil when the handler found nothing -- it still flows
// to classify so an active head resolves.
func (p *Publisher) Publish(ctx context.Context, name string, owner *common.Owner, alert *Alert) error {
	if owner == nil {
		return errors.New("owner must not be nil")
	}

	compactionKey, err := CompactionKey(name, owner)
	if err != nil {
		return err
	}

	head, err := p.alerts.GetCompactionHead(ctx, compactionKey)
	if err != nil {
		return err
	}

	published, err := classify(alert, head, p.repeat, time.Now())
	if err != nil {
		return err
	}
	if published == nil {
		return nil
	}

	if _, err := p.alerts.Produce(ctx, published, producer.ProduceOptions{
		RoutingKey:    published.RoutingKey(),
		CompactionKey: compactionKey,
	}); err != nil {
		return err
	}

	if statusChanged(published, head) {
		p.log(ctx, published)
	}
	return nil
}

func (p *Publisher) log(ctx context.Context, published *Alert) {
	if published.Status == StatusResolved {
		p.logger.InfoContext(ctx, published.Message,
			"alert", published.Name, "owner", published.Owner.Name)
		return
	}
	p.logger.WarnContext(ctx, published.Message,
		"detail", published.Detail, "hint", published.Hint,
		"alert", published.Name, "owner", published.Owner.Name, "severity", published.Severity)
}
