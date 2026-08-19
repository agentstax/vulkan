package consumer

import (
	"context"

	"github.com/agentstax/vulkan/pkg/consumer/exceptionconsumer"
	"github.com/agentstax/vulkan/pkg/consumer/messageconsumer"
	"github.com/agentstax/vulkan/pkg/worker"
	"github.com/agentstax/vulkan/pkg/worker/cronscheduler"
	"github.com/agentstax/vulkan/pkg/worker/cursoradvancer"
	"github.com/agentstax/vulkan/pkg/worker/janitor"
	"github.com/agentstax/vulkan/pkg/worker/manager"
	"github.com/agentstax/vulkan/pkg/worker/metricscollector"
)

// every group-owned row is declared here rather than at Register, so the
// definition that runs a row is the one that creates it and a second Consume
// re-creates whatever a crash lost
func (i *ConsumerInstance[Message]) newManagerRunner(ctx context.Context, consumerFunc ConsumerFunc[Message]) (*manager.Runner, error) {
	groupDefinitions, err := i.newGroupDefinitions(consumerFunc)
	if err != nil {
		return nil, err
	}
	topicDefinitions, err := i.newTopicDefinitions()
	if err != nil {
		return nil, err
	}

	provisioners := make([]worker.Provisioner, 0, len(groupDefinitions)+len(topicDefinitions))
	for _, definition := range groupDefinitions {
		if err := definition.Declare(ctx, i.Owner); err != nil {
			return nil, err
		}
		provisioners = append(provisioners, definition)
	}
	provisioners = append(provisioners, topicDefinitions...)

	managerDefinition, err := manager.NewManagerDefinition(i.ds, &manager.ManagerConfig{
		Logger: i.Config.Logger,
		Retry:  i.Config.Retry,
	}, provisioners...)
	if err != nil {
		return nil, err
	}

	if err := managerDefinition.Declare(ctx, i.Owner); err != nil {
		return nil, err
	}

	return manager.NewRunner(managerDefinition, i.Owner, &manager.RunnerConfig{
		Logger: i.Logger,
	})
}

// one frontier per group, with committed advancing behind it
func (i *ConsumerInstance[Message]) newGroupDefinitions(consumerFunc ConsumerFunc[Message]) ([]worker.Definition, error) {
	message, err := messageconsumer.NewMessageConsumerDefinition(i.ds, consumerFunc, i.abandonedEvents, toMessageConsumerConfig(i.Config))
	if err != nil {
		return nil, err
	}

	exception, err := exceptionconsumer.NewExceptionConsumerDefinition(i.ds, consumerFunc, i.abandonedEvents, toExceptionConsumerConfig(i.Config))
	if err != nil {
		return nil, err
	}

	cursorAdvancerDefinition, err := cursoradvancer.NewCursorAdvancerDefinition(i.ds, &cursoradvancer.CursorAdvancerConfig{
		Logger: i.Logger,
		Retry:  i.Config.Retry,
	})
	if err != nil {
		return nil, err
	}

	return []worker.Definition{message, exception, cursorAdvancerDefinition}, nil
}

func (i *ConsumerInstance[Message]) newTopicDefinitions() ([]worker.Provisioner, error) {
	janitorDefinition, err := janitor.NewJanitorDefinition(i.ds, &janitor.JanitorConfig{
		Logger: i.Logger,
		Retry:  i.Config.Retry,
	})
	if err != nil {
		return nil, err
	}

	cronSchedulerDefinition, err := cronscheduler.NewCronSchedulerDefinition(i.ds, &cronscheduler.CronSchedulerConfig{
		Logger: i.Logger,
		Retry:  i.Config.Retry,
	})
	if err != nil {
		return nil, err
	}

	metricsCollectorDefinition, err := metricscollector.NewMetricsCollectorDefinition(i.ds, &metricscollector.MetricsCollectorConfig{
		Logger: i.Logger,
		Retry:  i.Config.Retry,
	})
	if err != nil {
		return nil, err
	}

	return []worker.Provisioner{cronSchedulerDefinition, metricsCollectorDefinition, janitorDefinition}, nil
}
