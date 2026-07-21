package main

import (
	"context"
	"fmt"
	"os"

	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/topic"
)

func main() {
	if err := run(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()

	type Message struct {
		data string
	}

	ds, err := datastore.NewPostgresDatastore(ctx, &datastore.PostgresConnectionConfig{
		User:     "example_user",
		Pass:     "example_password",
		Host:     "localhost",
		Port:     5432,
		Database: "example_db",
	})
	if err != nil {
		return err
	}

	topicProducerRegister, err := topic.Register(ctx, ds, &topic.Config{
		Name: "test.producerregister",
	})

	_, err = producer.NewMessageProducer[Message](topicProducerRegister, ds, &producer.MessageProducerConfig{})
	if err != nil {
		return err
	}

	return topic.Destroy(ctx, ds, "test.producerregister")
}
