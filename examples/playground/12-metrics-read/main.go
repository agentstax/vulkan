// Scenario 12 -- reading what the system measures about itself.
//
// A service that consumes orders while the manager's metrics collector
// measures the fleet, plus a loop printing the group's own gauges from
// the __system.metrics topic -- the pull side an ops dashboard would use.
//
// Concepts held before domain code (13): the 10 from scenario 08, plus
// ListMeasurements / ListMeasurementMessages, the MessageData envelope,
// and pkg/metrics for the metric names.
//
// Traps hit:
//   - Measurements exist only after the manager's metrics collector ticks
//     (30s default poll): the first prints are empty lists, and nothing on
//     ClientConfig or RegisterSystemConfig sets the collector's rate -- it
//     is worker metadata the client surface never reaches.
//   - ListMeasurements is every head fleet-wide; narrowing to one group is
//     a caller-side loop over each measurement's Attributes.
//   - Metric names live in pkg/metrics -- the vulkan package aliases none
//     of them, so the read side needs a second import.
//   - The series key for ListMeasurementMessages is
//     metrics.MeasurementKey(name, attributes); reusing a head's own
//     MessageKey is the only way to avoid guessing the attribute set.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/metrics"
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

	ds, err := datastore.NewPostgresDatastore(ctx, "example_user", "localhost", "example_db",
		&datastore.PostgresConnectionConfig{Pass: "example_password"})
	if err != nil {
		return err
	}
	defer ds.Close()

	client, err := vulkan.NewClient(ds, nil)
	if err != nil {
		return err
	}
	registered, err := client.RegisterTopic(ctx, "orders.placed", nil)
	if err != nil {
		return err
	}

	orders, err := client.RegisterProducer[OrderPlaced](ctx, registered.Name, nil)
	if err != nil {
		return err
	}
	for i := range 5 {
		if _, err := orders.Produce(ctx, &OrderPlaced{OrderId: fmt.Sprintf("ord-%d", i)}, nil); err != nil {
			return err
		}
	}

	ledger, err := client.RegisterConsumer[OrderPlaced](ctx, "ledger", registered.Name, nil)
	if err != nil {
		return err
	}

	group, groupCtx := errgroup.WithContext(ctx)
	group.Go(func() error { return client.RunManager(groupCtx) })
	group.Go(func() error {
		return ledger.Consume(groupCtx, func(ctx context.Context, order *OrderPlaced) error {
			fmt.Printf("recording %s\n", order.OrderId)
			return nil
		}, nil)
	})
	group.Go(func() error { return printLedgerMeasurements(groupCtx, client) })
	return group.Wait()
}

// printLedgerMeasurements prints the ledger group's gauges each tick, and
// one series' retained history once its head exists.
func printLedgerMeasurements(ctx context.Context, client *vulkan.Client) error {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}

		heads, err := client.ListMeasurements(ctx)
		if err != nil {
			return err
		}
		for _, head := range heads {
			measurement := head.Message
			if measurement.Attributes["group"] != "ledger" {
				continue
			}
			fmt.Printf("%s = %g\n", measurement.Name, measurement.Value)

			if measurement.Name == metrics.MetricCursorBacklog {
				history, err := client.ListMeasurementMessages(ctx, head.MessageKey, 5)
				if err != nil {
					return err
				}
				fmt.Printf("  backlog history: %d retained measurements\n", len(history))
			}
		}
	}
}
