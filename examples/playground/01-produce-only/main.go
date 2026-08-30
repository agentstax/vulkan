// Scenario 01 -- produce-only service.
//
// A web service that emits an event when an order is placed. It never
// consumes anything.
//
// Concepts held before domain code (7): datastore, MessageAdmin,
// RegisterSystem, topic name, the Message type's SchemaVersion, Producer[T],
// ProducerInstance.
//
// Traps hit:
//   - Nothing here runs topic upkeep (partition create-ahead, retention).
//     A deployment of only this binary silently accumulates until someone
//     runs `vulkan manager run`. No log line says so.
//   - RegisterSystem is required even though this program never reads
//     system state; forgetting it fails at RegisterTopic with VK0013.
//   - RegisterTopic accepts nil cfg (verified) -- the quickstart's
//     &topiccontroller.TopicConfig{} and its import are unnecessary.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/agentstax/vulkan/pkg/admin"
	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/topic"
)

type OrderPlacedV1 struct {
	OrderId string `json:"order_id"`
	Total   int64  `json:"total_cents"`
}

func (OrderPlacedV1) SchemaVersion() topic.SchemaVersion { return 1 }

func main() {
	if err := run(); err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()

	ds, err := datastore.NewPostgresDatastore(ctx, "example_user", "localhost", "example_db",
		&datastore.PostgresConnectionConfig{Pass: "example_password"})
	if err != nil {
		return err
	}
	defer ds.Close()

	// START - not needed every time
	messageAdmin, err := admin.NewMessageAdmin(ds, nil)
	if err != nil {
		return err
	}
	if err := messageAdmin.RegisterSystem(ctx, nil); err != nil {
		return err
	}

	registered, err := messageAdmin.RegisterTopic(ctx, "orders.placed", nil)
	if err != nil {
		return err
	}
	// END - not needed every time

	orderProducer, err := producer.NewProducer[OrderPlacedV1](ds, nil)
	if err != nil {
		return err
	}
	orders, err := orderProducer.Register(ctx, registered.Name)
	if err != nil {
		return err
	}

	produced, err := orders.Produce(ctx, &OrderPlacedV1{OrderId: "ord-1", Total: 4200}, producer.ProduceOptions{})
	if err != nil {
		return err
	}
	fmt.Printf("produced id=%d\n", produced.Id)
	return nil
}
