// Scenario 13 -- consuming __system.alerts as a pager feed.
//
// The built-in checks (partition_count, compaction_read_cost,
// worker_liveness) run as schedules under the manager and publish Alert
// messages; a consumer group on the alert topic is the push integration a
// Slack or PagerDuty hook would hang off. The checks are re-declared here
// at every-minute so a run has any chance of seeing one.
//
// Concepts held before domain code (12): the 7 from scenario 03, plus
// RegisterSystem, the three check JobConfigs and their cron expressions,
// and pkg/alert's TopicName and Alert. The checks run because Consume runs
// the manager.
//
// Traps hit:
//   - The default check schedules are @hourly; tightening them means
//     knowing all three JobConfig fields by name -- there is no single
//     "check interval" knob.
//   - Nothing can fire a test alert: on a healthy system this consumer
//     prints nothing, so the pager integration is unverifiable until
//     something actually breaks.
//   - alert.TopicName and alert.Alert live in pkg/alert, the JobConfigs
//     in three subpackages -- the vulkan package aliases none of them.
//   - The __system. prefix guard exists only on RegisterTopic; nothing
//     states whether a consumer group on a system topic is supported or
//     accidental.
//   - The group's cursor starts at head, so alerts active before startup
//     are never delivered -- ListAlerts is the read for current state, and
//     nothing points from one to the other.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/agentstax/vulkan/pkg/alert"
	"github.com/agentstax/vulkan/pkg/alert/compactionreadcost"
	"github.com/agentstax/vulkan/pkg/alert/partitioncount"
	"github.com/agentstax/vulkan/pkg/alert/workerliveness"
	vulkan "github.com/agentstax/vulkan/pkg/vulkan"
)

func main() {
	if err := run(); err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := vulkan.LifecycleContext(nil)
	defer stop()

	pool, err := vulkan.NewPostgresPool(ctx, "example_user", "example_password", "localhost", "example_db", nil)
	if err != nil {
		return err
	}
	defer pool.Close()

	client, err := vulkan.NewClient(ctx, pool, nil)
	if err != nil {
		return err
	}

	// newest declaration wins: every minute instead of the @hourly default
	if err := client.RegisterSystem(ctx, &vulkan.RegisterSystemConfig{
		PartitionCount:     &partitioncount.JobConfig{Expression: "* * * * *"},
		CompactionReadCost: &compactionreadcost.JobConfig{Expression: "* * * * *"},
		WorkerLiveness:     &workerliveness.JobConfig{Expression: "* * * * *"},
	}); err != nil {
		return err
	}

	// the pull side: what is active or resolved right now
	heads, err := client.ListAlerts(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("%d alert heads at startup\n", len(heads))

	pager, err := client.RegisterConsumer[alert.Alert](ctx, "alert-pager", alert.TopicName, nil)
	if err != nil {
		return err
	}

	return pager.Consume(ctx, func(ctx context.Context, foundAlert *alert.Alert) error {
		fmt.Printf("[%s] %s %s: %s -- %s\n",
			foundAlert.Severity, foundAlert.Status, foundAlert.Name, foundAlert.Message, foundAlert.Hint)
		return nil
	}, nil)
}
