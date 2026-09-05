// Scenario 06 -- a schedule.
//
// Nightly invoice run: register the schedule once with the message it
// produces and the topic it produces to, consume that topic like any
// other.
//
// Concepts held before domain code (12): the 7 from scenario 03, plus
// SchedulerHandle.Register[T] and the returned
// SchedulerInstance's Schedule
// verb, vulkan.MetaFromContext for the scheduled time, and the fact that
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

	vulkan "github.com/agentstax/vulkan/pkg/vulkan"
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
	invoices, err := client.Topic[InvoiceRun]("invoices").Register(ctx, nil)
	if err != nil {
		return err
	}

	nightly, err := client.Scheduler("invoices.nightly").Register(ctx, invoices.Name, "0 2 * * *", &InvoiceRun{Region: "eu"}, nil)
	if err != nil {
		return err
	}

	runs, err := client.Topic[InvoiceRun](invoices.Name).Group("invoice-runner").Register(ctx, nil)
	if err != nil {
		return err
	}

	group, groupCtx := errgroup.WithContext(ctx)
	group.Go(func() error { return nightly.Schedule(groupCtx) })
	group.Go(func() error {
		return runs.Consume(groupCtx, func(ctx context.Context, run *InvoiceRun) error {
			meta, _ := vulkan.MetaFromContext(ctx)
			fmt.Printf("invoicing %s for %s\n", run.Region, meta.ScheduledAt.Format("2006-01-02"))
			return nil
		}, nil)
	})
	return group.Wait()
}
