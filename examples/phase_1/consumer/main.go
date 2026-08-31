package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"math/rand/v2"
	"os"
	"sync/atomic"
	"time"

	"github.com/agentstax/vulkan/examples/phase_1/common"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
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

	groupPtr := flag.String("group", "learning.v1", "consumer group name")
	topicPtr := flag.String("topic", "learning.v1", "topic to consume from (must already be registered, e.g. via `just produce`)")
	processorSleepPtr := flag.Float64("processor-sleep", 0.1, "artifical sleep in consumer func for testing (in seconds)")
	failRatePtr := flag.Float64("fail-rate", 0.0, "artifical fail rate in consumer func for testing")
	crashAfterPtr := flag.Float64("crash-after", -1, "artificial crash after n attempts for testing")

	// must always parse
	flag.Parse()

	fmt.Printf("flag group: %s\n", *groupPtr)
	fmt.Printf("flag topic: %s\n", *topicPtr)
	fmt.Printf("flag processor sleep: %f\n", *processorSleepPtr)
	fmt.Printf("flag fail rate: %f\n", *failRatePtr)
	fmt.Printf("crash after: %f\n", *crashAfterPtr)

	// SETUP
	ctx, stop := vulkan.LifecycleContext(nil)
	defer stop()

	const concurrencyLimit = 5

	ds, err := iDatastore.NewPostgresDatastore(ctx, "example_user", "localhost", "example_db", &iDatastore.PostgresConnectionConfig{Pass: "example_password"})
	if err != nil {
		return err
	}
	defer ds.Close()

	client, err := vulkan.NewClient(ds, &vulkan.ClientConfig{AllowDestroy: true})
	if err != nil {
		return err
	}

	t, err := client.Topic(*topicPtr).Get(ctx)
	if err != nil {
		return err
	}
	if t == nil {
		return fmt.Errorf("topic %q is not registered -- `just produce` declares it\n", *topicPtr)
	}

	workInstance, err := client.RegisterConsumer[common.Work](ctx, *groupPtr, t.Name, &vulkan.ConsumerConfig{
		Message: &vulkan.MessageOptions{Timeout: 5 * time.Second, Retry: &vulkan.RetryPolicy{MaxRetries: 3}},
	})
	if err != nil {
		return err
	}

	// WORK
	var attempts atomic.Int64
	err = workInstance.Consume(ctx, func(ctx context.Context, work *common.Work) error {
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
	}, &vulkan.ConsumeOptions{
		BatchLimit:         10,
		QueueSize:          concurrencyLimit * 10,
		MessageConcurrency: concurrencyLimit,
		ClaimPollRate:      1 * time.Second,
		QueueMargin:        2 * time.Second,
		RecordMargin:       1 * time.Second,
	})
	if err != nil {
		return err
	}
	return nil
}
