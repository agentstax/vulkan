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
)

type PaymentRequestedV1 struct {
	OrderId string `json:"order_id"`
	Card    string `json:"card"` // "declined" | "gateway-down" | "settles-later" | anything else succeeds
}

// increment on breaking changes
func (PaymentRequestedV1) SchemaVersion() int { return 1 }

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

	paymentConsumer, err := consumer.NewConsumer(ds, &consumer.ConsumerConfig{
		Message: &common.MessageOptions{
			Timeout: 10 * time.Second,
			Retry:   &common.RetryPolicy{MaxRetries: 3, BaseDelay: 2 * time.Second},
		},
	})
	if err != nil {
		return err
	}
	payments, err := paymentConsumer.Register[PaymentRequestedV1](ctx, "charge-cards", "payments.requested", nil)
	if err != nil {
		return err
	}

	return payments.Consume(ctx, func(ctx context.Context, payment *PaymentRequestedV1) error {
		meta, _ := consumergroup.MetaFromContext(ctx)
		fmt.Printf("charging %s (message %d, attempt %d, delays %d)\n",
			payment.OrderId, meta.Id, meta.Attempts+1, meta.Delays)

		switch payment.Card {
		case "declined":
			// dead on this attempt; the cause lands in last_error
			return consumergroup.Terminal(errCardDeclined)
		case "gateway-down":
			// retried MaxRetries times with backoff
			return errGatewayDown
		case "settles-later":
			// runs again after the delay
			return consumergroup.Delay(untilSettlement())
		}
		return nil
	})
}

// untilSettlement is the wait until the bank's next 02:00 settlement.
func untilSettlement() time.Duration {
	now := time.Now()
	settlement := time.Date(now.Year(), now.Month(), now.Day(), 2, 0, 0, 0, now.Location())
	if !settlement.After(now) {
		settlement = settlement.AddDate(0, 0, 1)
	}
	return settlement.Sub(now)
}
