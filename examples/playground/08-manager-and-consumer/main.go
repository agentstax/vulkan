// Scenario 08 -- one binary running the manager and a consumer.
//
// The deployment most teams actually have: a single service process that
// consumes and also keeps the system's upkeep running, without a separate
// `vulkan manager run` process. Consume runs the system manager beside the
// session, so this file is scenario 03 unchanged -- the shape costs nothing
// to reach.
//
// Concepts held before domain code (7): the 7 from scenario 03. The manager
// is not one of them.
//
// Traps hit:
//   - What the manager covers versus what the consumer covers is still not
//     discoverable from the API -- it stops mattering here, because one
//     Consume covers both, but a produce-only binary still has no long
//     running call to carry upkeep.
//   - The opt-out is client-wide and named on the other side of the
//     question a user asks: a process that must NOT run upkeep sets
//     ClientConfig.DisableManager, and nothing at Consume says so.
package main

import (
	"context"
	"fmt"
	"os"

	vulkan "github.com/agentstax/vulkan/pkg/vulkan"
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
	shipping, err := client.Consumer("shipping", "orders.placed").Register[OrderPlaced](ctx, nil)
	if err != nil {
		return err
	}

	return shipping.Consume(ctx, func(ctx context.Context, order *OrderPlaced) error {
		fmt.Printf("shipping %s\n", order.OrderId)
		return nil
	}, nil)
}
