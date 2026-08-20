package consumer

import (
	"context"

	"github.com/agentstax/vulkan/pkg/consumergroup/cursoradvancer"
	"github.com/agentstax/vulkan/pkg/consumergroup/exceptionconsumer"
	"github.com/agentstax/vulkan/pkg/consumergroup/messageconsumer"
	"github.com/agentstax/vulkan/pkg/cron/scheduler"
	"github.com/agentstax/vulkan/pkg/metrics/collector"
	"github.com/agentstax/vulkan/pkg/topic/janitor"
	"github.com/agentstax/vulkan/pkg/worker"
	"github.com/agentstax/vulkan/pkg/worker/manager"
)

// every group-owned row is declared here rather than at Register, so the
// provisioner that runs a row is the one that declares it and a second
// Consume re-creates whatever a crash lost
func (i *ConsumerInstance[Message]) newManagerRunner(ctx context.Context, consumerFunc ConsumerFunc[Message]) (*manager.Runner, error) {
	groupProvisioners, err := i.newGroupProvisioners(ctx, consumerFunc)
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

	managerProvisioner, err := manager.NewManagerProvisioner(i.ds, &manager.ManagerConfig{
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
func (i *ConsumerInstance[Message]) newGroupProvisioners(ctx context.Context, consumerFunc ConsumerFunc[Message]) ([]worker.Provisioner, error) {
	message, err := messageconsumer.NewMessageConsumerProvisioner(i.ds, consumerFunc, i.abandonedEvents, toMessageConsumerConfig(i.Config))
	if err != nil {
		return nil, err
	}
	if err := message.Declare(ctx, i.Owner); err != nil {
		return nil, err
	}

	exception, err := exceptionconsumer.NewExceptionConsumerProvisioner(i.ds, consumerFunc, i.abandonedEvents, toExceptionConsumerConfig(i.Config))
	if err != nil {
		return nil, err
	}
	if err := exception.Declare(ctx, i.Owner); err != nil {
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
	janitorProvisioner, err := janitor.NewJanitorProvisioner(i.ds, &janitor.JanitorConfig{
		Logger: i.Logger,
		Retry:  i.Config.Retry,
	})
	if err != nil {
		return nil, err
	}

	cronSchedulerProvisioner, err := scheduler.NewCronSchedulerProvisioner(i.ds, &scheduler.CronSchedulerConfig{
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

	return []worker.Provisioner{cronSchedulerProvisioner, metricsCollectorProvisioner, janitorProvisioner}, nil
}
