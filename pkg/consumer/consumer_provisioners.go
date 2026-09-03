package consumer

import (
	"context"

	"github.com/agentstax/vulkan/pkg/consume/cursoradvancer"
	"github.com/agentstax/vulkan/pkg/consume/exceptionconsumer"
	consumejanitor "github.com/agentstax/vulkan/pkg/consume/janitor"
	"github.com/agentstax/vulkan/pkg/consume/messageconsumer"
	"github.com/agentstax/vulkan/pkg/metrics/collector"
	scheduleproducer "github.com/agentstax/vulkan/pkg/schedule/producer"
	topicjanitor "github.com/agentstax/vulkan/pkg/topic/janitor"
	"github.com/agentstax/vulkan/pkg/worker"
	"github.com/agentstax/vulkan/pkg/worker/manager"
)

// the group's consumer rows and their config document are declared by
// Register; the upkeep rows below are declared here, so a second Consume
// re-creates whatever a crash lost
func (i *ConsumerInstance[Message]) newManagerRunner(ctx context.Context, consumerFunc ConsumerFunc[Message], options *ConsumeOptions) (*manager.Runner, error) {
	groupProvisioners, err := i.newGroupProvisioners(ctx, consumerFunc, options)
	if err != nil {
		return nil, err
	}
	topicProvisioners, err := i.newTopicProvisioners()
	if err != nil {
		return nil, err
	}

	provisioners := make([]worker.Provisioner, 0, len(groupProvisioners)+len(topicProvisioners))
	provisioners = append(provisioners, groupProvisioners...)
	provisioners = append(provisioners, topicProvisioners...)

	managerProvisioner, err := manager.NewManagerProvisioner(i.ds, worker.NoInstanceTarget, &manager.ManagerConfig{
		Logger: i.Config.Logger,
		Retry:  i.Config.Retry,
	}, provisioners...)
	if err != nil {
		return nil, err
	}

	if err := managerProvisioner.Declare(ctx, i.Owner); err != nil {
		return nil, err
	}

	return manager.NewRunner(managerProvisioner, i.Owner, &manager.RunnerConfig{
		Logger: i.Logger,
	})
}

// one frontier per group, with committed advancing behind it. Each
// provisioner declares its own row before it joins the manager's list.
func (i *ConsumerInstance[Message]) newGroupProvisioners(ctx context.Context, consumerFunc ConsumerFunc[Message], options *ConsumeOptions) ([]worker.Provisioner, error) {
	message, err := messageconsumer.NewMessageConsumerProvisioner(i.ds, consumerFunc, i.topicVersion, i.metrics, toMessageConsumerConfig(i.Config, options))
	if err != nil {
		return nil, err
	}

	exception, err := exceptionconsumer.NewExceptionConsumerProvisioner(i.ds, consumerFunc, i.topicVersion, i.metrics, toExceptionConsumerConfig(i.Config, options))
	if err != nil {
		return nil, err
	}

	cursorAdvancerProvisioner, err := cursoradvancer.NewCursorAdvancerProvisioner(i.ds, &cursoradvancer.CursorAdvancerConfig{
		Logger: i.Logger,
		Retry:  i.Config.Retry,
	})
	if err != nil {
		return nil, err
	}
	if err := cursorAdvancerProvisioner.Declare(ctx, i.Owner); err != nil {
		return nil, err
	}

	return []worker.Provisioner{message, exception, cursorAdvancerProvisioner}, nil
}

func (i *ConsumerInstance[Message]) newTopicProvisioners() ([]worker.Provisioner, error) {
	topicJanitorProvisioner, err := topicjanitor.NewJanitorProvisioner(i.ds, &topicjanitor.JanitorConfig{
		Logger: i.Logger,
		Retry:  i.Config.Retry,
	})
	if err != nil {
		return nil, err
	}

	consumerGroupJanitorProvisioner, err := consumejanitor.NewJanitorProvisioner(i.ds, &consumejanitor.JanitorConfig{
		Logger: i.Logger,
		Retry:  i.Config.Retry,
	})
	if err != nil {
		return nil, err
	}

	scheduleProducerProvisioner, err := scheduleproducer.NewScheduleProducerProvisioner(i.ds, &scheduleproducer.ScheduleProducerConfig{
		Logger: i.Logger,
		Retry:  i.Config.Retry,
	})
	if err != nil {
		return nil, err
	}

	metricsCollectorProvisioner, err := collector.NewMetricsCollectorProvisioner(i.ds, &collector.MetricsCollectorConfig{
		Logger: i.Logger,
		Retry:  i.Config.Retry,
	})
	if err != nil {
		return nil, err
	}

	return []worker.Provisioner{scheduleProducerProvisioner, metricsCollectorProvisioner, topicJanitorProvisioner, consumerGroupJanitorProvisioner}, nil
}
