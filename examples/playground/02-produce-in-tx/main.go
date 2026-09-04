// Scenario 02 -- produce inside the caller's own transaction.
//
// Insert the order row and the OrderPlaced message atomically; then the
// multi-topic form where the caller owns the transaction.
//
// Concepts held before domain code (8): the 5 from scenario 01, plus
// ProducerFunc, vulkan.Tx, InTransaction / ProduceInTx.
//
// Traps hit:
//   - The order of statements matters (produce last: it holds a lock on
//     consumer progress until commit) and nothing in the types says so.
//   - InTransaction does not retry; the caller must know that and own the
//     loop (user-settled).
package main

import (
	"context"
	"fmt"
	"os"

	vulkan "github.com/agentstax/vulkan/pkg/vulkan"
	"github.com/jackc/pgx/v5/pgxpool"
)

type OrderPlacedV1 struct {
	OrderId string `json:"order_id"`
}

// increment on breaking changes
func (OrderPlacedV1) SchemaVersion() int { return 1 }

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
	ctx, stop := vulkan.LifecycleContext(nil)
	defer stop()

	pool, err := vulkan.NewPostgresPool(ctx, "example_user", "example_password", "localhost", "example_db", nil)
	if err != nil {
		return err
	}
	defer pool.Close()

	if err := createOrdersTable(ctx, pool); err != nil {
		return err
	}

	client, err := vulkan.NewClient(ctx, pool, nil)
	if err != nil {
		return err
	}
	_, err = client.Topic("orders.placed").Register(ctx, nil)
	if err != nil {
		return err
	}
	_, err = client.Topic("inventory.reserved").Register(ctx, nil)
	if err != nil {
		return err
	}

	orders, err := client.Producer("orders.placed").Register[OrderPlacedV1](ctx, nil)
	if err != nil {
		return err
	}
	inventory, err := client.Producer("inventory.reserved").Register[InventoryReservedV1](ctx, nil)
	if err != nil {
		return err
	}

	// one topic: the message's own transaction carries the business write
	produced, err := orders.ProduceFunc(ctx,
		func(ctx context.Context, tx vulkan.Tx) (*OrderPlacedV1, error) {
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
	if err := client.InTransaction(ctx, func(ctx context.Context, tx vulkan.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO playground_orders (id) VALUES ($1) ON CONFLICT DO NOTHING`, "ord-3"); err != nil {
			return err
		}
		if _, err := orders.ProduceInTx(ctx, tx, &OrderPlacedV1{OrderId: "ord-3"}, nil); err != nil {
			return err
		}
		_, err := inventory.ProduceInTx(ctx, tx, &InventoryReservedV1{OrderId: "ord-3", Sku: "sku-9"}, nil)
		return err
	}); err != nil {
		return err
	}
	fmt.Println("two topics committed together")
	return nil
}

// createOrdersTable stands in for a business table an application would
// already have -- without it the scenario cannot run against a fresh database.
func createOrdersTable(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS playground_orders (id text PRIMARY KEY)`)
	return err
}
