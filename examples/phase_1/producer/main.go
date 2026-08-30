package main

import (
	"context"
	"flag"
	"fmt"
	"math/rand/v2"
	"os"

	"github.com/agentstax/vulkan/examples/phase_1/common"
	"github.com/agentstax/vulkan/pkg/admin"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/producer"
	topiccontroller "github.com/agentstax/vulkan/pkg/topic/controller"
	"github.com/google/uuid"
)

func main() {
	if err := run(); err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}
}

func run() error {
	// FLAGS

	// -count n
	countPtr := flag.Int("count", 1, "number of messages produced")

	// -routing-key key
	routingKeyPtr := flag.String("routing-key", "", "routing key attached to each message (optional)")

	// -topic name
	topicPtr := flag.String("topic", "learning.v1", "topic to publish to (this command declares it -- the other examples only read it)")

	// must always parse
	flag.Parse()

	fmt.Printf("count: %d, routing-key: %q, topic: %q\n", *countPtr, *routingKeyPtr, *topicPtr)

	// SETUP
	ctx := context.Background()

	ds, err := iDatastore.NewPostgresDatastore(ctx, "example_user", "localhost", "example_db", &iDatastore.PostgresConnectionConfig{Pass: "example_password"})
	if err != nil {
		return err
	}
	defer ds.Close()

	mAdmin, err := admin.NewMessageAdmin(ds, &admin.MessageAdminConfig{AllowDestroy: true})
	if err != nil {
		return err
	}
	if err := mAdmin.RegisterSystem(ctx, nil); err != nil {
		return err
	}

	t, err := mAdmin.RegisterTopic(ctx, *topicPtr, &topiccontroller.TopicConfig{})
	if err != nil {
		return err
	}

	wp, err := producer.NewProducer(ds, nil)
	if err != nil {
		return err
	}
	wpInstance, err := wp.Register[common.Work](ctx, t.Name)
	if err != nil {
		return err
	}

	// WORK
	for range *countPtr {
		produced, err := wpInstance.ProduceFunc(ctx, func(ctx context.Context, tx producer.Tx, _ uuid.UUID) (*common.Work, error) {
			work, err := common.NewWork(rand.IntN(100), "admin@example.com")
			if err != nil {
				return nil, err
			}

			return work, nil
		}, producer.ProduceOptions{RoutingKey: *routingKeyPtr})
		if err != nil {
			return err
		}

		fmt.Printf("successfully produced message: work=%s id=%d\n", produced.Message.Id, produced.Id)
	}
	return nil
}
