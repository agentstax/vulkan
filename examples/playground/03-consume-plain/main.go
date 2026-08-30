// Scenario 03 -- consume, plain.
//
// A service that only handles OrderPlaced. It owns no topic and needs no
// admin verbs -- yet has to build a MessageAdmin and call RegisterSystem
// just to check the topic exists, and then Register repeats the name and
// version anyway.
//
// Concepts held before domain code (9): datastore, LifecycleContext,
// MessageAdmin, RegisterSystem, GetTopic, SchemaVersion, Consumer[T],
// consumer group name, bindings (nil).
//
// Traps hit:
//   - context.Background() into Consume fails with VK0002; the fix is a
//     Vulkan-specific ctx constructor the user must discover.
//   - The MessageAdmin + RegisterSystem detour: GetTopic is the only reason
//     admin is imported, and Register would resolve the topic itself --
//     the pre-check adds a nil branch for something Register already errors on.
//   - Register's nil last argument is the binding set; a reader cannot
//     tell what nil means from the call.
//   - Two `ctx` shapes in one file: the lifecycle ctx for Consume, and the
//     per-message ctx handed to the handler.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/consumer"
	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/topic"
)

type OrderPlacedV1 struct {
	OrderId string `json:"order_id"`
	Total   int64  `json:"total_cents"`
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

	orderConsumer, err := consumer.NewConsumer[OrderPlacedV1](ds, nil)
	if err != nil {
		return err
	}
	receipts, err := orderConsumer.Register(ctx, "email-receipts", "orders.placed", topic.SchemaVersion(1), nil)
	if err != nil {
		return err
	}

	return receipts.Consume(ctx, func(ctx context.Context, order *OrderPlacedV1) error {
		fmt.Printf("receipt for %s: %d cents\n", order.OrderId, order.Total)
		return nil
	})
}
