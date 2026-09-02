// Scenario 08 -- one binary running the manager and a consumer.
//
// The deployment most teams actually have: a single service process that
// consumes and also keeps the system's upkeep running, without a separate
// `vulkan manager run` process.
//
// Concepts held before domain code (11): the 8 from scenario 03, plus
// client.RunManager, errgroup/goroutine wiring, and the knowledge that a
// consumer already runs its own topic's upkeep so the manager is for
// everything else.
//
// Traps hit:
//   - Two long-running Run/Consume calls, two goroutines, one lifecycle
//     ctx: the composition is on the user (errgroup is the honest answer
//     and is not in the library's examples).
//   - What the manager covers versus what the consumer covers is not
//     discoverable from the API -- the quickstart's CAUTION aside and the
//     client guide's manager section are where it is written.
//   - The manager needs the control-plane tables to exist but registers
//     no topic itself -- it assumes some producer's RegisterTopic (or
//     RegisterSystem) already ran.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/agentstax/vulkan/pkg/datastore"
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

	pool, err := datastore.NewPostgresPool(ctx, "example_user", "example_password", "localhost", "example_db", nil)
	if err != nil {
		return err
	}
	defer pool.Close()

	ds, err := datastore.NewPostgresDatastore(ctx, pool, nil)
	if err != nil {
		return err
	}

	client, err := vulkan.NewClient(ds, nil)
	if err != nil {
		return err
	}
	shipping, err := client.RegisterConsumer[OrderPlaced](ctx, "shipping", "orders.placed", nil)
	if err != nil {
		return err
	}

	group, ctx := errgroup.WithContext(ctx)
	group.Go(func() error { return client.RunManager(ctx) })
	group.Go(func() error {
		return shipping.Consume(ctx, func(ctx context.Context, order *OrderPlaced) error {
			fmt.Printf("shipping %s\n", order.OrderId)
			return nil
		}, nil)
	})
	return group.Wait()
}
