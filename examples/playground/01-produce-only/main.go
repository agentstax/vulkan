// Scenario 01 -- produce-only service.
//
// A web service that emits an event when an order is placed. It never
// consumes anything.
//
// Concepts held before domain code (7): datastore, MessageAdmin,
// RegisterSystem, topic name, SchemaVersion, Producer[T], ProducerInstance.
//
// Traps hit:
//   - Nothing here runs topic upkeep (partition create-ahead, retention).
//     A deployment of only this binary silently accumulates until someone
//     runs `vulkan manager run`. No log line says so.
//   - RegisterSystem is required even though this program never reads
//     system state; forgetting it fails at RegisterTopic with VK0013.
//   - SchemaVersion(1) is spelled twice (RegisterTopic, Register).
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

type OrderPlaced struct {
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
	ctx := context.Background()

	ds, err := datastore.NewPostgresDatastore(ctx, "example_user", "localhost", "example_db",
		&datastore.PostgresConnectionConfig{Pass: "example_password"})
	if err != nil {
		return err
	}
	defer ds.Close()

	messageAdmin, err := admin.NewMessageAdmin(ds, nil)
	if err != nil {
		return err
	}
	if err := messageAdmin.RegisterSystem(ctx, nil); err != nil {
		return err
	}

	registered, err := messageAdmin.RegisterTopic(ctx, "orders.placed", topic.SchemaVersion(1), nil)
	if err != nil {
		return err
	}

	orderProducer, err := producer.NewProducer[OrderPlaced](ds, nil)
	if err != nil {
		return err
	}
	orders, err := orderProducer.Register(ctx, registered.Name, topic.SchemaVersion(1))
	if err != nil {
		return err
	}

	produced, err := orders.Produce(ctx, &OrderPlaced{OrderId: "ord-1", Total: 4200}, producer.ProduceOptions{})
	if err != nil {
		return err
	}
	fmt.Printf("produced id=%d\n", produced.Id)
	return nil
}
