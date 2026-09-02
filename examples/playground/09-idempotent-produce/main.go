// Scenario 09 -- idempotent produce with a caller-supplied key.
//
// A webhook receiver: the upstream retries on any non-2xx, so the same
// event arrives more than once and must be stored once.
//
// Concepts held before domain code (7): the produce set from scenario 01,
// plus IdempotencyKey as an opaque string and ProduceResult.Duplicate.
//
// Traps hit:
//   - Duplicate is a field on a success result, not an error; a caller
//     that only checks err treats the duplicate as a fresh produce.
//   - A caller-supplied key opts the call out of batching (documented in
//     the field comment only).
//   - The idempotency window is IdempotencyKeyTTL on the TOPIC (1h) --
//     an upstream retrying after an hour double-stores, silently.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/agentstax/vulkan/pkg/datastore"
	vulkan "github.com/agentstax/vulkan/pkg/vulkan"
)

type WebhookEvent struct {
	EventId string `json:"event_id"`
	Kind    string `json:"kind"`
}

// increment on breaking changes
func (WebhookEvent) SchemaVersion() int { return 1 }

func main() {
	if err := run(); err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()

	pool, err := datastore.NewPostgresPool(ctx, "example_user", "example_password", "localhost", "example_db", nil)
	if err != nil {
		return err
	}
	defer pool.Close()

	client, err := vulkan.NewClient(ctx, pool, nil)
	if err != nil {
		return err
	}
	registered, err := client.RegisterTopic(ctx, "webhooks.received", nil)
	if err != nil {
		return err
	}

	webhooks, err := client.RegisterProducer[WebhookEvent](ctx, registered.Name, nil)
	if err != nil {
		return err
	}

	// the upstream delivers evt_123 twice
	for range 2 {
		event := &WebhookEvent{EventId: "evt_123", Kind: "charge.succeeded"}
		produced, err := webhooks.Produce(ctx, event, &vulkan.ProduceOptions{
			IdempotencyKey: event.EventId,
		})
		if err != nil {
			return err
		}
		fmt.Printf("id=%d duplicate=%v\n", produced.Id, produced.Duplicate)
	}
	return nil
}
