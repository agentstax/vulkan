// Scenario 12 -- reading what the system measures about itself.
//
// A service that consumes orders while the manager's metrics collector
// measures the fleet, plus a loop printing the group's own gauges from
// the __system.metrics topic -- the pull side an ops dashboard would use.
//
// Concepts held before domain code (11): the 7 from scenario 03, plus a
// GroupMetricsHandle, its typed CursorBacklog selector, and retained Latest /
// History measurements. The collector runs because Consume runs the manager --
// errgroup is here for the print loop, not for it.
//
// Traps hit:
//   - A retained measurement exists only after the manager's metrics collector
//     ticks (30s default poll): Latest initially returns nil, and nothing on
//     ClientConfig or RegisterSystemConfig sets the collector's rate -- it
//     is worker metadata the client surface never reaches.
//   - Latest is the newest collected value, not live state. Measurement.At is
//     the observation time; Snapshot asks the source tables what is true now.
//   - The Group metrics handle supplies topic and group attributes. A typed
//     selector avoids copying the metric's wire name or assembling its key.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	vulkan "github.com/agentstax/vulkan/pkg/vulkan"
	"golang.org/x/sync/errgroup"
)

type OrderPlaced struct {
	OrderId string `json:"order_id"`
}

// increment on breaking changes
func (OrderPlaced) SchemaVersion() int { return 1 }

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
	registered, err := client.Topic[OrderPlaced]("orders.placed").Register(ctx, nil)
	if err != nil {
		return err
	}

	orders, err := client.Topic[OrderPlaced](registered.Name).Producer().Register(ctx, nil)
	if err != nil {
		return err
	}
	for i := range 5 {
		if _, err := orders.Produce(ctx, &OrderPlaced{OrderId: fmt.Sprintf("ord-%d", i)}, nil); err != nil {
			return err
		}
	}

	ledger, err := client.Topic[OrderPlaced](registered.Name).Group("ledger").Register(ctx, nil)
	if err != nil {
		return err
	}

	group, groupCtx := errgroup.WithContext(ctx)
	group.Go(func() error {
		return ledger.Consume(groupCtx, func(ctx context.Context, order *OrderPlaced) error {
			fmt.Printf("recording %s\n", order.OrderId)
			return nil
		}, nil)
	})
	groupMetrics := client.Topic[OrderPlaced](registered.Name).Group("ledger").Metrics()
	group.Go(func() error { return printLedgerMeasurements(groupCtx, groupMetrics) })
	return group.Wait()
}

// printLedgerMeasurements prints the ledger group's collected backlog and
// retained history each tick once its first measurement exists.
func printLedgerMeasurements(ctx context.Context, groupMetrics *vulkan.GroupMetricsHandle) error {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	backlogMetric := groupMetrics.CursorBacklog()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}

		backlog, err := backlogMetric.Latest(ctx)
		if err != nil {
			return err
		}
		if backlog == nil {
			fmt.Println("backlog has not been collected yet")
			continue
		}

		history, err := backlogMetric.History(ctx, 5)
		if err != nil {
			return err
		}
		fmt.Printf("backlog = %g (collected %s, %d retained measurements)\n",
			backlog.Value, backlog.At.Local().Format(time.RFC3339), len(history))
	}
}
