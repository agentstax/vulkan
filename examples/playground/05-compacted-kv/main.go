// Scenario 05 -- a compacted topic used as a key/value store.
//
// Device configuration: one current document per device id. Read the
// current value, write a new one, and increment a counter safely under
// concurrent writers (read-modify-write).
//
// Concepts held before domain code (14): the 6 from scenario 01, plus
// MessageKey, CompactionOptions (+NewCompactionOptions), Rank,
// InTransaction, GetCompactionHeadInTx, ProduceInTx, MessageData, and the
// Topic handle for reads outside a transaction.
//
// Traps hit:
//   - "Compacted" is a per-message option, not a topic property: every
//     produce must pass Compaction or the message silently is not one
//     version of the key -- it is its own message forever.
//   - Get-by-key outside a transaction is on the topic handle
//     (Topic.CompactionHead) and inside one is on the producer
//     (GetCompactionHeadInTx). Two homes for one read, though both now
//     take the topic by name.
//   - CAS exists only as a pattern: InTransaction + GetCompactionHeadInTx
//     (FOR UPDATE) + ProduceInTx. Nothing named Update/Put says so.
//     JetStream KV: Get -> revision; Update(key, value, revision).
//   - History (the key's prior versions) is Topic.ListKeyMessages -- a
//     third verb, third name.
//   - Rank is a commitment, not a hint; the zero value (arrival order) is
//     what most users want and NewCompactionOptions(0) reads like "no rank".
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/agentstax/vulkan/pkg/datastore"
	vulkan "github.com/agentstax/vulkan/pkg/vulkan"
)

type DeviceConfig struct {
	DeviceId string `json:"device_id"`
	Interval int    `json:"interval_seconds"`
	Restarts int    `json:"restarts"`
}

// increment on breaking changes
func (DeviceConfig) SchemaVersion() int { return 1 }

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

	ds, err := datastore.NewPostgresDatastore(ctx, pool, nil)
	if err != nil {
		return err
	}

	client, err := vulkan.NewClient(ds, nil)
	if err != nil {
		return err
	}
	registered, err := client.RegisterTopic(ctx, "devices.config", nil)
	if err != nil {
		return err
	}

	configs, err := client.RegisterProducer[DeviceConfig](ctx, registered.Name, nil)
	if err != nil {
		return err
	}

	compaction, err := vulkan.NewCompactionOptions(0)
	if err != nil {
		return err
	}

	// Put
	_, err = configs.Produce(ctx, &DeviceConfig{DeviceId: "dev-7", Interval: 30},
		&vulkan.ProduceOptions{MessageKey: "dev-7", Compaction: compaction})
	if err != nil {
		return err
	}

	// Get (outside a transaction) -- the topic handle's read
	current, err := client.Topic(registered.Name).CompactionHead[DeviceConfig](ctx, "dev-7")
	if err != nil {
		return err
	}
	fmt.Printf("current: id=%d interval=%d restarts=%d\n", current.Id, current.Message.Interval, current.Message.Restarts)

	// Update (compare-and-set): lock the head, write the next version
	if err := vulkan.InTransaction(ctx, ds, func(ctx context.Context, tx vulkan.Tx) error {
		head, err := configs.GetCompactionHeadInTx(ctx, tx, "dev-7")
		if err != nil {
			return err
		}
		next := *head.Message
		next.Restarts++
		_, err = configs.ProduceInTx(ctx, tx,
			func(ctx context.Context, tx vulkan.Tx, _ string) (*DeviceConfig, error) {
				return &next, nil
			}, &vulkan.ProduceOptions{MessageKey: "dev-7", Compaction: compaction})
		return err
	}); err != nil {
		return err
	}

	// History
	versions, err := client.Topic(registered.Name).ListKeyMessages[DeviceConfig](ctx, "dev-7", 10)
	if err != nil {
		return err
	}
	for _, version := range versions {
		fmt.Printf("version id=%d restarts=%d\n", version.Id, version.Message.Restarts)
	}
	return nil
}
