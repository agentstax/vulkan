package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"math/rand/v2"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/agentstax/vulkan/examples/phase_1/common"
	"github.com/agentstax/vulkan/pkg/consumer"
	"github.com/agentstax/vulkan/pkg/consumer/datastore"
)

func main() {
	// FLAGS

	processorSleepPtr := flag.Float64("processor-sleep", 0.1, "artifical sleep in consumer func for testing (in seconds)")
	shutdownSleepPtr := flag.Float64("shutdown-sleep", 1.0, "artifical sleep on graceful shutdown for testing (in seconds)")
	failRatePtr := flag.Float64("fail-rate", 0.0, "artifical fail rate in consumer func for testing")
	crashAfterPtr := flag.Int("crash-after", -1, "artificial crash after n attempts for testing")

	// must always parse
	flag.Parse()

	fmt.Printf("flag processor sleep: %f\n", *processorSleepPtr)
	fmt.Printf("flag shutdown sleep: %f\n", *shutdownSleepPtr)
	fmt.Printf("flag fail rate: %f\n", *failRatePtr)
	fmt.Printf("crash after: %d\n", *crashAfterPtr)

	// SETUP
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

	workConsumer := consumer.NewWorkConsumer(datastore).WithBatchLimit(1).WithPollRate(1 * time.Second)
	workConsumer.WithCustomShutdown(func(ctx context.Context, workConsumer *consumer.WorkConsumer[common.Work]) error {
		if err := workConsumer.Datastore.Shutdown(ctx); err != nil {
			return err
		}

		// artifical sleep for testing functionality
		time.Sleep(time.Duration(*shutdownSleepPtr) * time.Second)

		return nil
	})

	// WORK
	var attempts atomic.Int64
	err = workConsumer.Consume(ctx, func(ctx context.Context, work common.Work) error {
		fmt.Printf("work processes start %s\n", work.Id)

		// artificial sleep
		time.Sleep(time.Duration(*processorSleepPtr) * time.Second)

		// artificial crash
		attempts.Add(1)
		if *crashAfterPtr > 0 && attempts.Load() >= int64(*crashAfterPtr) {
			fmt.Printf("crashing after: %d attempts\n", *crashAfterPtr)
			os.Exit(1)
		}

		// artificial fail rate
		if rand.Float64() < *failRatePtr {
			return errors.New("artificial failure from -fail-rate")
		}

		fmt.Printf("work processes end %s\n", work.Id)
		return nil
	})
	if err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}
}
