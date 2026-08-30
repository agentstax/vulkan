// Scenario 06 -- a schedule.
//
// Nightly invoice run: register the schedule once with the message it
// produces and the topic it produces to, consume that topic like any
// other.
//
// Concepts held before domain code (9): the consume set from scenario 03,
// plus Scheduler, its Register and Schedule, ScheduleConfig,
// consumergroup.MetaFromContext for the scheduled time, and the fact that
// Schedule runs the system manager.
//
// Traps hit:
//   - Nothing produces if no manager is running; Schedule is the manager,
//     so a register-and-exit program registers a schedule that never fires.
//   - The scheduled time is not in the payload (the payload never
//     changes) -- it is on the delivery's meta, reached through ctx.
//   - A one-off "run this in 10 minutes" is not this feature; it is
//     queue-native delayed delivery (future roadmap).
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/agentstax/vulkan/pkg/admin"
	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/consumer"
	"github.com/agentstax/vulkan/pkg/consumergroup"
	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/scheduler"
	"golang.org/x/sync/errgroup"
)

type InvoiceRun struct {
	Region string `json:"region"`
}

// increment on breaking changes
func (InvoiceRun) SchemaVersion() int { return 1 }

func main() {
	if err := run(); err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := common.LifecycleContext(nil)
	defer stop()

	ds, err := datastore.NewPostgresDatastore(ctx, "example_user", "localhost", "example_db",
		&datastore.PostgresConnectionConfig{Pass: "example_password"})
	if err != nil {
		return err
	}
	defer ds.Close()

	messageAdmin, err := admin.NewMessageAdmin(ds, nil)
	if err != nil {
		return err
	}
	if err := messageAdmin.RegisterSystem(ctx, nil); err != nil {
		return err
	}
	invoices, err := messageAdmin.RegisterTopic(ctx, "invoices", nil)
	if err != nil {
		return err
	}

	invoiceScheduler, err := scheduler.NewScheduler(ds, nil)
	if err != nil {
		return err
	}
	nightly, err := invoiceScheduler.Register[InvoiceRun](ctx, "invoices.nightly", "0 2 * * *", invoices.Name, &InvoiceRun{Region: "eu"}, nil)
	if err != nil {
		return err
	}

	invoiceConsumer, err := consumer.NewConsumer(ds, nil)
	if err != nil {
		return err
	}
	runs, err := invoiceConsumer.Register[InvoiceRun](ctx, "invoice-runner", invoices.Name, nil)
	if err != nil {
		return err
	}

	group, groupCtx := errgroup.WithContext(ctx)
	group.Go(func() error { return nightly.Schedule(groupCtx) })
	group.Go(func() error {
		return runs.Consume(groupCtx, func(ctx context.Context, run *InvoiceRun) error {
			meta, _ := consumergroup.MetaFromContext(ctx)
			fmt.Printf("invoicing %s for %s\n", run.Region, meta.ScheduledAt.Format("2006-01-02"))
			return nil
		})
	})
	return group.Wait()
}
