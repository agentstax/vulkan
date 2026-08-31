// Scenario 02 -- produce inside the caller's own transaction.
//
// Insert the order row and the OrderPlaced message atomically; then the
// multi-topic form where the caller owns the transaction.
//
// Concepts held before domain code (9): the 6 from scenario 01, plus
// ProducerFunc, vulkan.Tx, InTransaction / ProduceInTx.
//
// Traps hit:
//   - ProduceFunc's closure takes three params (ctx, tx, idempotencyKey);
//     the key is unused in the common case and is spelled `_` every time.
//   - A static payload inside InTransaction still costs a closure per
//     topic -- ProduceInTx has no value-taking form (ROADMAP gap).
//   - The order of statements matters (produce last: it holds a lock on
//     consumer progress until commit) and nothing in the types says so.
//   - InTransaction does not retry; the caller must know that and own the
//     loop (user-settled).
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/agentstax/vulkan/pkg/datastore"
	vulkan "github.com/agentstax/vulkan/pkg/vulkan"
)

type OrderPlacedV1 struct {
	OrderId string `json:"order_id"`
}

type InventoryReservedV1 struct {
	OrderId string `json:"order_id"`
	Sku     string `json:"sku"`
}

// increment on breaking changes
func (InventoryReservedV1) SchemaVersion() int { return 1 }

func main() {
	if err := run(); err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()

	ds, err := datastore.NewPostgresDatastore(ctx, "example_user", "localhost", "example_db",
		&datastore.PostgresConnectionConfig{Pass: "example_password"})
	if err != nil {
		return err
	}
	defer ds.Close()

	client, err := vulkan.NewClient(ds, nil)
	if err != nil {
		return err
	}
	_, err = client.RegisterTopic(ctx, "orders.placed", nil)
	if err != nil {
		return err
	}
	_, err = client.RegisterTopic(ctx, "inventory.reserved", nil)
	if err != nil {
		return err
	}

	orders, err := client.RegisterProducer[OrderPlacedV1](ctx, "orders.placed", nil)
	if err != nil {
		return err
	}
	inventory, err := client.RegisterProducer[InventoryReservedV1](ctx, "inventory.reserved", nil)
	if err != nil {
		return err
	}

	// one topic: the message's own transaction carries the business write
	produced, err := orders.ProduceFunc(ctx,
		func(ctx context.Context, tx vulkan.Tx, _ string) (*OrderPlacedV1, error) {
			if _, err := tx.Exec(ctx, `INSERT INTO playground_orders (id) VALUES ($1) ON CONFLICT DO NOTHING`, "ord-2"); err != nil {
				return nil, err
			}
			return &OrderPlacedV1{OrderId: "ord-2"}, nil
		}, nil)
	if err != nil {
		return err
	}
	fmt.Printf("produced id=%d\n", produced.Id)

	// two topics: the caller owns the transaction, each instance produces into it
	if err := vulkan.InTransaction(ctx, ds, func(ctx context.Context, tx vulkan.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO playground_orders (id) VALUES ($1) ON CONFLICT DO NOTHING`, "ord-3"); err != nil {
			return err
		}
		if _, err := orders.ProduceInTx(ctx, tx,
			func(ctx context.Context, tx vulkan.Tx, _ string) (*OrderPlacedV1, error) {
				return &OrderPlacedV1{OrderId: "ord-3"}, nil
			}, nil); err != nil {
			return err
		}
		_, err := inventory.ProduceInTx(ctx, tx,
			func(ctx context.Context, tx vulkan.Tx, _ string) (*InventoryReservedV1, error) {
				return &InventoryReservedV1{OrderId: "ord-3", Sku: "sku-9"}, nil
			}, nil)
		return err
	}); err != nil {
		return err
	}
	fmt.Println("two topics committed together")
	return nil
}

// increment on breaking changes
func (OrderPlacedV1) SchemaVersion() int { return 1 }
