// Scenario 09 -- idempotent produce with a caller-supplied key.
//
// A webhook receiver: the upstream retries on any non-2xx, so the same
// event arrives more than once and must be stored once.
//
// Concepts held before domain code (9): the produce set from scenario 01,
// plus IdempotencyKey as a uuid.UUID, the v7-not-v4 performance note, and
// ProduceResult.Duplicate.
//
// Traps hit:
//   - The key is a uuid.UUID, but the caller's natural key is the
//     upstream's event id (a string). The user must derive a UUID from it
//     deterministically (uuid.NewSHA1 over a namespace) -- and that yields
//     a v5, exactly the random-shaped key the docs warn against.
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

	"github.com/agentstax/vulkan/pkg/admin"
	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/google/uuid"
)

type WebhookEvent struct {
	EventId string `json:"event_id"`
	Kind    string `json:"kind"`
}

var webhookNamespace = uuid.MustParse("6ba7b810-9dad-11d1-80b4-00c04fd430c8")

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

	messageAdmin, err := admin.NewMessageAdmin(ds, nil)
	if err != nil {
		return err
	}
	if err := messageAdmin.RegisterSystem(ctx, nil); err != nil {
		return err
	}
	registered, err := messageAdmin.RegisterTopic(ctx, "webhooks.received", topic.SchemaVersion(1), nil)
	if err != nil {
		return err
	}

	webhookProducer, err := producer.NewProducer[WebhookEvent](ds, nil)
	if err != nil {
		return err
	}
	webhooks, err := webhookProducer.Register(ctx, registered.Name, topic.SchemaVersion(1))
	if err != nil {
		return err
	}

	// the upstream delivers evt_123 twice
	for range 2 {
		event := &WebhookEvent{EventId: "evt_123", Kind: "charge.succeeded"}
		produced, err := webhooks.Produce(ctx, event, producer.ProduceOptions{
			IdempotencyKey: uuid.NewSHA1(webhookNamespace, []byte(event.EventId)),
		})
		if err != nil {
			return err
		}
		fmt.Printf("id=%d duplicate=%v\n", produced.Id, produced.Duplicate)
	}
	return nil
}
