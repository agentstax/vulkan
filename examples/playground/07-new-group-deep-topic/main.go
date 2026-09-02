// Scenario 07 -- a new consumer group on a topic with deep history.
//
// A fraud-scoring service is added a year after orders.placed went live.
// It wants live traffic only.
//
// Concepts held before domain code (8): the 7 from scenario 03, plus
// ConsumerConfig.Start (vulkan.Head()).
//
// Traps hit:
//   - Start is read once, when Register creates the group's cursor row. A
//     group that already exists keeps its position; changing Start later
//     changes nothing and nothing says so. Kafka's auto.offset.reset has
//     the same rule -- moving an existing group is Group.Rewind, PROPOSED.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/agentstax/vulkan/pkg/datastore"
	vulkan "github.com/agentstax/vulkan/pkg/vulkan"
)

type OrderPlaced struct {
	OrderId string `json:"order_id"`
	Total   int64  `json:"total_cents"`
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

	scoring, err := client.RegisterConsumer[OrderPlaced](ctx, "fraud-scoring", "orders.placed", &vulkan.ConsumerConfig{
		Start: vulkan.Head(),
	})
	if err != nil {
		return err
	}

	return scoring.Consume(ctx, func(ctx context.Context, order *OrderPlaced) error {
		fmt.Printf("scoring %s\n", order.OrderId)
		return nil
	}, nil)
}
