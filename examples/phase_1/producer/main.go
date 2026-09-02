package main

import (
	"context"
	"flag"
	"fmt"
	"math/rand/v2"
	"os"

	"github.com/agentstax/vulkan/examples/phase_1/common"
	vulkan "github.com/agentstax/vulkan/pkg/vulkan"
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

	pool, err := vulkan.NewPostgresPool(ctx, "example_user", "example_password", "localhost", "example_db", nil)
	if err != nil {
		return err
	}
	defer pool.Close()

	client, err := vulkan.NewClient(ctx, pool, &vulkan.ClientConfig{AllowDestroy: true})
	if err != nil {
		return err
	}

	t, err := client.RegisterTopic(ctx, *topicPtr, &vulkan.TopicConfig{})
	if err != nil {
		return err
	}

	wpInstance, err := client.RegisterProducer[common.Work](ctx, t.Name, nil)
	if err != nil {
		return err
	}

	// WORK
	for range *countPtr {
		produced, err := wpInstance.ProduceFunc(ctx, func(ctx context.Context, tx vulkan.Tx) (*common.Work, error) {
			work, err := common.NewWork(rand.IntN(100), "admin@example.com")
			if err != nil {
				return nil, err
			}

			return work, nil
		}, &vulkan.ProduceOptions{RoutingKey: *routingKeyPtr})
		if err != nil {
			return err
		}

		fmt.Printf("successfully produced message: work=%s id=%d\n", produced.Message.Id, produced.Id)
	}
	return nil
}
