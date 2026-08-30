// Scenario 10 -- keyed ordering: what a same-key consumer actually sees.
//
// Account balance updates for one account must apply in order and never
// overlap. The producer keys by account; the consumer runs concurrently.
//
// Concepts held before domain code (11): the produce set from scenario 01,
// plus MessageKey, MessageOptions.Concurrency (ConcurrencyOrdered), the
// consumer's MessageConcurrency, ConcurrencyOverride, and the
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

	"github.com/agentstax/vulkan/pkg/admin"
	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/consumer"
	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/topic"
	"golang.org/x/sync/errgroup"
)

type BalanceChanged struct {
	AccountId string `json:"account_id"`
	Delta     int64  `json:"delta_cents"`
}

func (BalanceChanged) SchemaVersion() topic.SchemaVersion { return 1 }

func main() {
	if err := run(); err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := common.LifecycleContext(nil)
	defer stop()

	ds, err := datastore.NewPostgresDatastore(ctx, "example_user", "localhost", "example_db",
		&datastore.PostgresConnectionConfig{Pass: "example_password"})
	if err != nil {
		return err
	}
	defer ds.Close()

	messageAdmin, err := admin.NewMessageAdmin(ds, nil)
	if err != nil {
		return err
	}
	if err := messageAdmin.RegisterSystem(ctx, nil); err != nil {
		return err
	}
	registered, err := messageAdmin.RegisterTopic(ctx, "accounts.balance", nil)
	if err != nil {
		return err
	}

	balanceProducer, err := producer.NewProducer[BalanceChanged](ds, &producer.ProducerConfig{
		Message: &common.MessageOptions{Concurrency: common.ConcurrencyOrdered},
	})
	if err != nil {
		return err
	}
	balances, err := balanceProducer.Register(ctx, registered.Name)
	if err != nil {
		return err
	}

	for _, delta := range []int64{100, -30, 55} {
		_, err := balances.Produce(ctx, &BalanceChanged{AccountId: "acct-1", Delta: delta},
			producer.ProduceOptions{MessageKey: "acct-1"})
		if err != nil {
			return err
		}
	}

	ledgerConsumer, err := consumer.NewConsumer[BalanceChanged](ds, &consumer.ConsumerConfig{
		MessageConcurrency: 8,
	})
	if err != nil {
		return err
	}
	ledger, err := ledgerConsumer.Register(ctx, "ledger", registered.Name, nil)
	if err != nil {
		return err
	}

	// the first delivery of -30 fails; under ordered, +55 waits for its retry
	var failedOnce atomic.Bool

	group, ctx := errgroup.WithContext(ctx)
	group.Go(func() error {
		return ledger.Consume(ctx, func(ctx context.Context, change *BalanceChanged) error {
			if change.Delta == -30 && failedOnce.CompareAndSwap(false, true) {
				return errors.New("ledger row locked")
			}
			fmt.Printf("%s %+d\n", change.AccountId, change.Delta)
			return nil
		})
	})
	return group.Wait()
}
