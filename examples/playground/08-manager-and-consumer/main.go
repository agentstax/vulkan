// Scenario 08 -- one binary running the manager and a consumer.
//
// The deployment most teams actually have: a single service process that
// consumes and also keeps the system's upkeep running, without a separate
// `vulkan manager run` process.
//
// Concepts held before domain code (11): the consume set from scenario 03,
// plus SystemManager, its Run, errgroup/goroutine wiring, and the
// knowledge that a consumer already runs its own topic's upkeep so the
// manager is for everything else.
//
// Traps hit:
//   - Two long-running Run/Consume calls, two goroutines, one lifecycle
//     ctx: the composition is on the user (errgroup is the honest answer
//     and is not in the library's examples).
//   - What the manager covers versus what the consumer covers is not
//     discoverable from the API -- the quickstart's CAUTION aside is the
//     only place it is written.
//   - The manager needs RegisterSystem to have run; a consumer does not.
//     Whether this binary should call it is a guess.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/consumer"
	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/systemmanager"
	"github.com/agentstax/vulkan/pkg/topic"
	"golang.org/x/sync/errgroup"
)

type OrderPlaced struct {
	OrderId string `json:"order_id"`
}

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

	manager, err := systemmanager.NewSystemManager(ds, nil)
	if err != nil {
		return err
	}

	orderConsumer, err := consumer.NewConsumer[OrderPlaced](ds, nil)
	if err != nil {
		return err
	}
	shipping, err := orderConsumer.Register(ctx, "shipping", "orders.placed", topic.SchemaVersion(1), nil)
	if err != nil {
		return err
	}

	group, ctx := errgroup.WithContext(ctx)
	group.Go(func() error { return manager.Run(ctx) })
	group.Go(func() error {
		return shipping.Consume(ctx, func(ctx context.Context, order *OrderPlaced) error {
			fmt.Printf("shipping %s\n", order.OrderId)
			return nil
		})
	})
	return group.Wait()
}
