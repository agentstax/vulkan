// Scenario 03 -- consume, plain.
//
// A service that only handles OrderPlaced. It owns no topic and needs no
// admin verbs -- RegisterConsumer resolves the topic by name itself.
//
// Concepts held before domain code (7): datastore, LifecycleContext,
// the Message type's SchemaVersion, Client, RegisterConsumer[T], consumer
// group name, bindings (nil).
//
// Traps hit:
//   - context.Background() into Consume fails with VK0002; the fix is a
//     Vulkan-specific ctx constructor the user must discover.
//   - RegisterConsumer's nil last argument is the binding set; a reader
//     cannot tell what nil means from the call.
//   - Two `ctx` shapes in one file: the lifecycle ctx for Consume, and the
//     per-message ctx handed to the handler.
package main

import (
	"context"
	"fmt"
	"os"

	vulkan "github.com/agentstax/vulkan/pkg/vulkan"
	"github.com/agentstax/vulkan/pkg/datastore"
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

	receipts, err := client.RegisterConsumer[OrderPlacedV1](ctx, "email-receipts", "orders.placed", nil, nil)
	if err != nil {
		return err
	}

	return receipts.Consume(ctx, func(ctx context.Context, order *OrderPlacedV1) error {
		fmt.Printf("receipt for %s: %d cents\n", order.OrderId, order.Total)
		return nil
	})
}
