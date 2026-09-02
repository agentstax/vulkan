// Scenario 03 -- consume, plain.
//
// A service that only handles OrderPlaced. It owns no topic and needs no
// admin verbs -- RegisterConsumer resolves the topic by name itself.
//
// Concepts held before domain code (8): connection pool, datastore,
// LifecycleContext, the Message type's SchemaVersion, Client,
// RegisterConsumer[T], consumer group name, the nil group config (whole
// topic, defaults).
//
// Traps hit:
//   - context.Background() into Consume fails with VK0002; the fix is a
//     Vulkan-specific ctx constructor the user must discover.
//   - RegisterConsumer's nil config consumes the whole topic -- bindings
//     live on ConsumerConfig.Bindings, and nothing at the call says so.
//   - Two `ctx` shapes in one file: the lifecycle ctx for Consume, and the
//     per-message ctx handed to the handler.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/agentstax/vulkan/pkg/datastore"
	vulkan "github.com/agentstax/vulkan/pkg/vulkan"
)

type OrderPlacedV1 struct {
	OrderId string `json:"order_id"`
	Total   int64  `json:"total_cents"`
}

// increment on breaking changes
func (OrderPlacedV1) SchemaVersion() int { return 1 }

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

	receipts, err := client.RegisterConsumer[OrderPlacedV1](ctx, "email-receipts", "orders.placed", nil)
	if err != nil {
		return err
	}

	return receipts.Consume(ctx, func(ctx context.Context, order *OrderPlacedV1) error {
		fmt.Printf("receipt for %s: %d cents\n", order.OrderId, order.Total)
		return nil
	}, nil)
}
