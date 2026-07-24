package main

import (
	"context"
	"flag"
	"fmt"
	"math/rand/v2"
	"os"

	"github.com/agentstax/vulkan/examples/phase_1/common"
	"github.com/agentstax/vulkan/pkg/admin"
	coredatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/google/uuid"
)

func main() {
	// FLAGS

	// -count n
	countPtr := flag.Int("count", 1, "number of messages produced")

	// -routing-key key
	routingKeyPtr := flag.String("routing-key", "", "routing key attached to each message (optional)")

	// -topic name
	topicPtr := flag.String("topic", "learning.v1", "topic to publish to (auto-registered if new)")

	// must always parse
	flag.Parse()

	fmt.Printf("count: %d, routing-key: %q, topic: %q\n", *countPtr, *routingKeyPtr, *topicPtr)

	// SETUP
	ctx := context.Background()

	ds, err := coredatastore.NewPostgresDatastore(ctx, &coredatastore.PostgresConnectionConfig{
		User:     "example_user",
		Pass:     "example_password",
		Host:     "localhost",
		Port:     5432,
		Database: "example_db",
	})
	if err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}
	defer ds.Close()

	mAdmin, err := admin.NewMessageAdmin(ds, &admin.MessageAdminConfig{AllowDestroy: true})
	if err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}
	if err := mAdmin.RegisterSystem(ctx); err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}

	t, err := mAdmin.RegisterTopic(ctx, *topicPtr, &topic.Config{})
	if err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}

	wp, err := producer.NewMessageProducer[common.Work](t.Name, ds, &producer.MessageProducerConfig{DisableGracefulShutdown: true})
	if err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}
	if err := wp.Register(ctx); err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}

	// WORK
	for range *countPtr {
		work, err := wp.ProduceFunc(ctx, func(ctx context.Context, tx producer.Tx, _ uuid.UUID) (*common.Work, error) {
			work, err := common.NewWork(rand.IntN(100), "admin@example.com")
			if err != nil {
				return nil, err
			}

			return work, nil
		}, producer.ProduceOptions{RoutingKey: *routingKeyPtr})
		if err != nil {
			fmt.Println(err.Error())
			os.Exit(1)
		}

		fmt.Printf("successfully produced message: %s\n", work.Id)
	}
}
