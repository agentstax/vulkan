package main

import (
	"context"
	"flag"
	"fmt"
	"math/rand/v2"
	"os"

	"github.com/agentstax/vulkan/examples/phase_1/common"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/producer/datastore"
)

func main() {
	// FLAGS

	// -count n
	countPtr := flag.Int("count", 1, "number of messages produced")

	// must always parse
	flag.Parse()

	fmt.Printf("count: %d\n", *countPtr)

	// SETUP
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

	// WORK
	for range *countPtr {

		work, err := common.NewWork(rand.IntN(100), "admin@example.com")
		if err != nil {
			fmt.Println(err.Error())
			os.Exit(1)
		}

		if err = producer.Produce(ctx, work); err != nil {
			fmt.Println(err.Error())
			os.Exit(1)
		}

		fmt.Printf("successfully produced message %s\n", work.Id)
	}
}
