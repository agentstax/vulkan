// Scenario 04 -- consume with retry and dead-lettering.
//
// A payment handler: a declined card will never succeed (terminal), the
// gateway being down will (retry), and "the bank settles at 02:00" should
// wait without counting as a failure (delay).
//
// Concepts held before domain code (10): the 7 from scenario 03, plus
// MessageOptions, RetryPolicy, and the ClientConfig.Retry vs
// ConsumerConfig.Message.Retry distinction.
//
// Traps hit:
//   - ClientConfig.Retry is the client's own Postgres retry;
//     ConsumerConfig.Message.Retry is message redelivery. Both are
//     *vulkan.RetryPolicy, one config apart.
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

	vulkan "github.com/agentstax/vulkan/pkg/vulkan"
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
	ctx, stop := vulkan.LifecycleContext(nil)
	defer stop()

	pool, err := vulkan.NewPostgresPool(ctx, "example_user", "example_password", "localhost", "example_db", nil)
	if err != nil {
		return err
	}
	defer pool.Close()

	client, err := vulkan.NewClient(ctx, pool, nil)
	if err != nil {
		return err
	}
	payments, err := client.Topic[PaymentRequestedV1]("payments.requested").Group("charge-cards").Register(ctx, &vulkan.ConsumerConfig{
		Message: &vulkan.MessageOptions{
			Timeout: 10 * time.Second,
			Retry:   &vulkan.RetryPolicy{MaxRetries: 3, BaseDelay: 2 * time.Second},
		},
	})

	if err != nil {
		return err
	}

	return payments.Consume(ctx, func(ctx context.Context, payment *PaymentRequestedV1) error {
		meta, _ := vulkan.MetaFromContext(ctx)
		fmt.Printf("charging %s (message %d, attempt %d, delays %d)\n",
			payment.OrderId, meta.Id, meta.Attempts+1, meta.Delays)

		switch payment.Card {
		case "declined":
			// dead on this attempt; the cause lands in last_error
			return vulkan.Terminal(errCardDeclined)
		case "gateway-down":
			// retried MaxRetries times with backoff
			return errGatewayDown
		case "settles-later":
			// runs again after the delay
			return vulkan.Delay(untilSettlement())
		}
		return nil
	}, nil)
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
