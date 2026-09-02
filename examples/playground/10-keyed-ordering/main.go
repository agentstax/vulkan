// Scenario 10 -- keyed ordering: what a same-key consumer actually sees.
//
// Account balance updates for one account must apply in order and never
// overlap. The producer keys by account; the consumer runs concurrently.
//
// Concepts held before domain code (10): the produce set from scenario 01,
// plus MessageKey, MessageOptions.Concurrency (ConcurrencyOrdered), the
// session's ConsumeOptions.MessageConcurrency, the group's
// ConcurrencyOverride, and the
// "ordered = every same-key message in id order, one at a time, through
// failures" semantics.
//
// Traps hit:
//   - A message key alone orders nothing: MessageConcurrency > 1 delivers
//     two same-key messages at once unless Concurrency is exclusive or
//     ordered -- and that is a per-MESSAGE option the producer sets, not a
//     topic or group property (ConcurrencyOverride on the consumer is the
//     group-wide form).
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync/atomic"

	"github.com/agentstax/vulkan/pkg/datastore"
	vulkan "github.com/agentstax/vulkan/pkg/vulkan"
)

type BalanceChanged struct {
	AccountId string `json:"account_id"`
	Delta     int64  `json:"delta_cents"`
}

// increment on breaking changes
func (BalanceChanged) SchemaVersion() int { return 1 }

func main() {
	if err := run(); err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := vulkan.LifecycleContext(nil)
	defer stop()

	pool, err := datastore.NewPostgresPool(ctx, "example_user", "example_password", "localhost", "example_db", nil)
	if err != nil {
		return err
	}
	defer pool.Close()

	client, err := vulkan.NewClient(ctx, pool, nil)
	if err != nil {
		return err
	}
	registered, err := client.RegisterTopic(ctx, "accounts.balance", nil)
	if err != nil {
		return err
	}

	balances, err := client.RegisterProducer[BalanceChanged](ctx, registered.Name, &vulkan.ProducerConfig{
		Message: &vulkan.MessageOptions{Concurrency: vulkan.ConcurrencyOrdered},
	})
	if err != nil {
		return err
	}

	for _, delta := range []int64{100, -30, 55} {
		_, err := balances.Produce(ctx, &BalanceChanged{AccountId: "acct-1", Delta: delta},
			&vulkan.ProduceOptions{MessageKey: "acct-1"})
		if err != nil {
			return err
		}
	}

	ledger, err := client.RegisterConsumer[BalanceChanged](ctx, "ledger", registered.Name, nil)
	if err != nil {
		return err
	}

	// the first delivery of -30 fails; under ordered, +55 waits for its retry.
	// atomic: MessageConcurrency 8 runs handlers on 8 goroutines -- ordered
	// serializes this one key, not the flag's memory visibility.
	var failedOnce atomic.Bool

	return ledger.Consume(ctx, func(ctx context.Context, change *BalanceChanged) error {
		if change.Delta == -30 && failedOnce.CompareAndSwap(false, true) {
			return errors.New("ledger row locked")
		}
		fmt.Printf("%s %+d\n", change.AccountId, change.Delta)
		return nil
	}, &vulkan.ConsumeOptions{MessageConcurrency: 8})
}
