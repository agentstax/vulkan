// Scenario 04 -- consume with retry and dead-lettering.
//
// A payment handler: a declined card will never succeed (terminal), the
// gateway being down will (retry), and "the bank settles at 02:00" should
// wait without counting as a failure (delay).
//
// Concepts held before domain code (12): the 9 from scenario 03, plus
// MessageOptions, RetryPolicy, and the ConsumerConfig.Retry vs
// ConsumerConfig.Message.Retry distinction.
//
// Traps hit:
//   - The handler has two outcomes: nil or error. Every error is an
//     exception row retried MaxRetries times; the declined card burns three
//     attempts and a backoff curve before it is dead. SETTLED 2026-08-29:
//     the runner will honour diagnostic Permanent -> terminal.
//   - No way to say "run me again after 02:00, this is not a failure".
//     SETTLED 2026-08-29: consumergroup.Delay(d) writing can_run_after and
//     bumping exception_queue.delays, capped by RetryPolicy.MaxDelays.
//   - ConsumerConfig.Retry is the consumer's own Postgres retry;
//     ConsumerConfig.Message.Retry is message redelivery. Both are
//     *common.RetryPolicy, both sit on the same config.
//   - Reading which attempt this is needs MetaFromContext -- the comma-ok
//     is always true inside a handler, yet every handler writes it.
//   - There is no dead-letter read verb on the consumer or admin; "what is
//     dead" is a psql query against exception_queue_<id>.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/consumer"
	"github.com/agentstax/vulkan/pkg/consumergroup"
	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/topic"
)

type PaymentRequested struct {
	OrderId string `json:"order_id"`
	Card    string `json:"card"` // "declined" | "gateway-down" | "settles-later" | anything else succeeds
}

var (
	errCardDeclined = errors.New("card declined")
	errGatewayDown  = errors.New("could not reach gateway")
)

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

	paymentConsumer, err := consumer.NewConsumer[PaymentRequested](ds, &consumer.ConsumerConfig{
		Message: &common.MessageOptions{
			Timeout: 10 * time.Second,
			Retry:   &common.RetryPolicy{MaxRetries: 3, BaseDelay: 2 * time.Second},
		},
	})
	if err != nil {
		return err
	}
	payments, err := paymentConsumer.Register(ctx, "charge-cards", "payments.requested", topic.SchemaVersion(1), nil)
	if err != nil {
		return err
	}

	return payments.Consume(ctx, func(ctx context.Context, payment *PaymentRequested) error {
		meta, _ := consumergroup.MetaFromContext(ctx)
		fmt.Printf("charging %s (message %d)\n", payment.OrderId, meta.Id)

		switch payment.Card {
		case "declined":
			// terminal in intent; retried 3 times in practice
			return errCardDeclined
		case "gateway-down":
			// transient -- retry is what we want
			return errGatewayDown
		case "settles-later":
			// wanted: come back after 02:00 without counting an attempt.
			// today the only choices are succeed (lose it) or fail (burn one).
			return errors.New("bank settles at 02:00")
		}
		return nil
	})
}
