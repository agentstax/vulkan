package main

import (
	"context"
	"fmt"
	"os"

	"github.com/agentstax/vulkan/examples/phase_1/common"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/producer/datastore"
)

func main() {
	ctx := context.Background()

	pgConnectionParams := &datastore.PostgresConnectionParams{
		User:     "example_user",
		Pass:     "example_password",
		Host:     "localhost",
		Port:     5432,
		Database: "example_db",
	}

	datastore, err := datastore.NewPostgresDatastore[common.Work](ctx, pgConnectionParams)
	if err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}

	producer, err := producer.NewWorkProducer(datastore)
	if err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}

	work, err := common.NewWork(123, "admin@example.com")
	if err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}

	if err = producer.Produce(ctx, work); err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}
}
