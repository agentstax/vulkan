package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/agentstax/vulkan/examples/phase_1/common"
	"github.com/agentstax/vulkan/pkg/consumer"
	"github.com/agentstax/vulkan/pkg/consumer/datastore"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

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

	consumer, err := consumer.NewWorkConsumer(datastore, 1, 5*time.Second)
	if err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}

	err = consumer.Consume(ctx, func(ctx context.Context, work common.Work) error {
		fmt.Println(work.Email)
		return nil
	})
	if err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}
}
