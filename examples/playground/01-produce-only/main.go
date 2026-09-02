// Scenario 01 -- produce-only service.
//
// A web service that emits an event when an order is placed. It never
// consumes anything.
//
// Concepts held before domain code (5): connection pool, Client, topic
// name, the Message type's SchemaVersion, RegisterProducer[T].
//
// Traps hit:
//   - Nothing here runs topic upkeep (partition create-ahead, retention).
//     A deployment of only this binary accumulates until someone runs
//     `vulkan manager run`. RegisterProducer now warns VK0063 naming the
//     unclaimed topic_janitor, so it is no longer silent -- but the warn
//     is the only thing that says so, and it is not an error.
//   - The pool is the one constructor before anything vulkan owns
//     [0633] [0636]. It buys the DATABASE_URL path and one pool per
//     application, and it costs a concept on every scenario's count.
package main

import (
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

	client, err := vulkan.NewClient(ctx, pool, nil)
	if err != nil {
		return err
	}

	registered, err := client.RegisterTopic(ctx, "orders.placed", nil)
	if err != nil {
		return err
	}

	orders, err := client.RegisterProducer[OrderPlacedV1](ctx, registered.Name, nil)
	if err != nil {
		return err
	}

	produced, err := orders.Produce(ctx, &OrderPlacedV1{OrderId: "ord-1", Total: 4200}, nil)
	if err != nil {
		return err
	}
	fmt.Printf("produced id=%d\n", produced.Id)
	return nil
}
