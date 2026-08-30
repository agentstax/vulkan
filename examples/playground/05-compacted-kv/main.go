// Scenario 05 -- a compacted topic used as a key/value store.
//
// Device configuration: one current document per device id. Read the
// current value, write a new one, and increment a counter safely under
// concurrent writers (read-modify-write).
//
// Concepts held before domain code (13): the produce set from scenario 01,
// plus MessageKey, CompactionOptions (+NewCompactionOptions), Rank,
// InTransaction, GetCompactionHeadInTx, ProduceInTx, MessageRow, and for
// reads outside a transaction the separate CompactionController with a
// topic id.
//
// Traps hit:
//   - "Compacted" is a per-message option, not a topic property: every
//     produce must pass Compaction or the message silently is not one
//     version of the key -- it is its own message forever.
//   - Get-by-key outside a transaction is on a different object
//     (compaction controller, keyed by topic id, not name) from the
//     producer that writes. Get-by-key inside a transaction is on the
//     producer. Two homes for one read.
//   - CAS exists only as a pattern: InTransaction + GetCompactionHeadInTx
//     (FOR UPDATE) + ProduceInTx. Nothing named Update/Put says so.
//     JetStream KV: Get -> revision; Update(key, value, revision).
//   - History (the key's prior versions) is ListKeyMessages on the
//     controller -- a third verb, third name.
//   - Rank is a commitment, not a hint; the zero value (arrival order) is
//     what most users want and NewCompactionOptions(0) reads like "no rank".
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/agentstax/vulkan/pkg/admin"
	compactioncontroller "github.com/agentstax/vulkan/pkg/compaction/controller"
	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/producer"
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
	registered, err := messageAdmin.RegisterTopic(ctx, "devices.config", nil)
	if err != nil {
		return err
	}

	configProducer, err := producer.NewProducer(ds, nil)
	if err != nil {
		return err
	}
	configs, err := configProducer.Register[DeviceConfig](ctx, registered.Name)
	if err != nil {
		return err
	}

	compaction, err := producer.NewCompactionOptions(0)
	if err != nil {
		return err
	}

	// Put
	_, err = configs.Produce(ctx, &DeviceConfig{DeviceId: "dev-7", Interval: 30},
		producer.ProduceOptions{MessageKey: "dev-7", Compaction: compaction})
	if err != nil {
		return err
	}

	// Get (outside a transaction) -- a different object, keyed by topic id
	heads, err := compactioncontroller.NewCompactionController(ds, nil)
	if err != nil {
		return err
	}
	current, err := heads.GetHead[DeviceConfig](ctx, registered.Id, "dev-7")
	if err != nil {
		return err
	}
	fmt.Printf("current: id=%d interval=%d restarts=%d\n", current.Id, current.Message.Interval, current.Message.Restarts)

	// Update (compare-and-set): lock the head, write the next version
	if err := producer.InTransaction(ctx, ds, func(ctx context.Context, tx producer.Tx) error {
		head, err := configs.GetCompactionHeadInTx(ctx, tx, "dev-7")
		if err != nil {
			return err
		}
		next := *head.Message
		next.Restarts++
		_, err = configs.ProduceInTx(ctx, tx,
			func(ctx context.Context, tx producer.Tx, _ string) (*DeviceConfig, error) {
				return &next, nil
			}, producer.ProduceOptions{MessageKey: "dev-7", Compaction: compaction})
		return err
	}); err != nil {
		return err
	}

	// History
	versions, err := heads.ListKeyMessages[DeviceConfig](ctx, registered.Id, "dev-7", 10)
	if err != nil {
		return err
	}
	for _, version := range versions {
		fmt.Printf("version id=%d restarts=%d\n", version.Id, version.Message.Restarts)
	}
	return nil
}
