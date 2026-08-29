// Scenario 10 -- keyed ordering: what a same-key consumer actually sees.
//
// Account balance updates for one account must apply in order and never
// overlap. The producer keys by account; the consumer runs concurrently.
//
// Concepts held before domain code (11): the produce set from scenario 01,
// plus MessageKey, MessageOptions.Concurrency (ConcurrencyExclusive), the
// consumer's MessageConcurrency, ConcurrencyOverride, and the
// "exclusive = only the key's most recent head runs" semantics.
//
// Traps hit:
//   - A message key alone orders nothing: MessageConcurrency > 1 delivers
//     two same-key messages at once unless Concurrency: exclusive is set --
//     and exclusive is a per-MESSAGE option the producer sets, not a topic or
//     group property (ConcurrencyOverride on the consumer is the group-wide
//     form).
//   - Exclusive is exclusivity, not order across failures: a same-key delivery
//     that errors leaves through the exception window and its retry does
//     not hold the key, so the NEXT same-key message runs before the failed
//     one's retry. For a balance stream that reorders deltas. Strict per-key
//     FIFO is a documented proposal, not shipped (concepts/ordering).
//   - The const comment on ConcurrencyExclusive ("only the key's most recent
//     head runs") describes exclusive+compaction, not exclusive alone -- without
//     compaction every message runs, oldest first. The doc site has it
//     right; the code comment misleads.
//   - Kafka users expect "same key, same partition, in order"; the
//     statement of what Vulkan guarantees per key lives in a concepts page,
//     not in any type.
package main

import (
	"context"
	"fmt"
	"os"

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
	registered, err := messageAdmin.RegisterTopic(ctx, "accounts.balance", topic.SchemaVersion(1), nil)
	if err != nil {
		return err
	}

	balanceProducer, err := producer.NewProducer[BalanceChanged](ds, &producer.ProducerConfig{
		Message: &common.MessageOptions{Concurrency: common.ConcurrencyExclusive},
	})
	if err != nil {
		return err
	}
	balances, err := balanceProducer.Register(ctx, registered.Name, topic.SchemaVersion(1))
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
	ledger, err := ledgerConsumer.Register(ctx, "ledger", registered.Name, topic.SchemaVersion(1), nil)
	if err != nil {
		return err
	}

	group, ctx := errgroup.WithContext(ctx)
	group.Go(func() error {
		return ledger.Consume(ctx, func(ctx context.Context, change *BalanceChanged) error {
			fmt.Printf("%s %+d\n", change.AccountId, change.Delta)
			return nil
		})
	})
	return group.Wait()
}
