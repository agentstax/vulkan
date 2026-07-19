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
	"github.com/agentstax/vulkan/pkg/concurrency"
	"github.com/agentstax/vulkan/pkg/consumer"
	coredatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/topic"
)

func main() {
	// FLAGS

	groupPtr := flag.String("group", "learning.v1", "consumer group name")
	topicPtr := flag.String("topic", "learning.v1", "topic to consume from (auto-registered if new)")
	processorSleepPtr := flag.Float64("processor-sleep", 0.1, "artifical sleep in consumer func for testing (in seconds)")
	shutdownSleepPtr := flag.Float64("shutdown-sleep", 1.0, "artifical sleep on graceful shutdown for testing (in seconds)")
	failRatePtr := flag.Float64("fail-rate", 0.0, "artifical fail rate in consumer func for testing")
	crashAfterPtr := flag.Float64("crash-after", -1, "artificial crash after n attempts for testing")

	// must always parse
	flag.Parse()

	fmt.Printf("flag group: %s\n", *groupPtr)
	fmt.Printf("flag topic: %s\n", *topicPtr)
	fmt.Printf("flag processor sleep: %f\n", *processorSleepPtr)
	fmt.Printf("flag shutdown sleep: %f\n", *shutdownSleepPtr)
	fmt.Printf("flag fail rate: %f\n", *failRatePtr)
	fmt.Printf("crash after: %f\n", *crashAfterPtr)

	// SETUP
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	const concurrencyLimit = 5

	pressureQueue, err := concurrency.NewPressureQueue[consumer.MessageRow](concurrencyLimit * 10)
	if err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}

	workerPoolLimiter, err := concurrency.NewWorkerPoolLimiter(concurrencyLimit)
	if err != nil {
		os.Exit(1)
	}

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

	t, err := topic.Register(ctx, ds, topic.Config{Name: *topicPtr})
	if err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}

	workConsumer, err := consumer.NewMessageConsumer[common.Work](*groupPtr, t, pressureQueue, workerPoolLimiter, ds, &consumer.MessageConsumerConfig{
		Type:          consumer.CURSOR,
		BatchLimit:    10,
		MaxAttempts:   3,
		ClaimPollRate: 1 * time.Second,
		WorkTimeout:   5 * time.Second,
		QueueMargin:   2 * time.Second,
		AckMargin:     1 * time.Second,
	})
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	workConsumer.WithShutdown(func(ctx context.Context, workConsumer *consumer.MessageConsumer[common.Work]) error {
		if err := workConsumer.Datastore.Shutdown(ctx); err != nil {
			return err
		}

		// artifical sleep for testing functionality
		time.Sleep(time.Duration(*shutdownSleepPtr) * time.Second)

		return nil
	}).WithShutdownTimeout(10 * time.Second)

	if err := workConsumer.Register(ctx); err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}

	// WORK
	var attempts atomic.Int64
	err = workConsumer.Consume(ctx, func(ctx context.Context, work *common.Work) error {
		// TODO - think through how users can log or confirm if a run is a retry or not. Maybe add info into context?

		fmt.Printf("work processes start %s\n", work.Id)

		// artificial sleep
		time.Sleep(time.Duration(*processorSleepPtr) * time.Second)

		// artificial crash
		attempts.Add(1)
		if *crashAfterPtr > 0 && attempts.Load() >= int64(*crashAfterPtr) {
			fmt.Printf("crashing after: %f attempts\n", *crashAfterPtr)
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
