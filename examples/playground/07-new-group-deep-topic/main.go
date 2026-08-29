// Scenario 07 -- a new consumer group on a topic with deep history.
//
// A fraud-scoring service is added a year after orders.placed went live.
// It wants live traffic only.
//
// Concepts held before domain code: identical to scenario 03 -- there is
// nothing to hold, because there is nothing to say.
//
// Traps hit:
//   - The cursor of a new group starts at 0. On a topic with millions of
//     retained messages this handler scores a year of orders before it
//     sees one from today, and nothing warns.
//   - No Register option, no config field, no admin verb expresses "start
//     from now" / "start from this id" / "start from this time". Kafka
//     auto.offset.reset=latest, JetStream DeliverNew, Pub/Sub seek --
//     every peer answers this on day one.
//   - The replay guide's RewindGroup is PROPOSED; the same verb pointed
//     forward would be this feature.
//   - Workaround today: RetentionTTL on the topic (a producer-side,
//     all-groups decision) or accepting the backlog.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/consumer"
	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/topic"
)

type OrderPlaced struct {
	OrderId string `json:"order_id"`
	Total   int64  `json:"total_cents"`
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

	scoringConsumer, err := consumer.NewConsumer[OrderPlaced](ds, nil)
	if err != nil {
		return err
	}

	// wanted: something here saying "from now". nothing exists.
	scoring, err := scoringConsumer.Register(ctx, "fraud-scoring", "orders.placed", topic.SchemaVersion(1), nil)
	if err != nil {
		return err
	}

	return scoring.Consume(ctx, func(ctx context.Context, order *OrderPlaced) error {
		fmt.Printf("scoring %s\n", order.OrderId)
		return nil
	})
}
