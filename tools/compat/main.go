package main

// compat lab: the PINNED release's side of the cross-version compatibility
// matrix. This module's vulkan dependency is whatever go.mod pins -- the
// prior release at a checkpoint, the working tree in dry-runs -- while the
// database it runs against was migrated by the working tree. It drives ONLY
// the public API: the compatibility surface is the public surface.
//
// -expect states the verdict the working tree's registry declares for this
// pinned build:
//   round-trip -> every step past the pinned build is additive; the full
//                 register/produce/consume lifecycle must succeed
//   refused    -> a breaking step is past the pinned build; producer
//                 Register must refuse with ErrSchemaNewerThanBuild
// A mismatch in either direction is the failure this lab exists to catch:
// a declared-additive step that breaks the old binary, or a gate that no
// longer refuses what a declaration says it must.

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"sync/atomic"
	"time"

	"github.com/agentstax/vulkan/pkg/admin"
	"github.com/agentstax/vulkan/pkg/consumer"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/migrate"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/topic"
)

const messageCount = 5

type event struct{ Sequence int }

func main() {
	expect := flag.String("expect", "round-trip", `declared verdict: "round-trip" | "refused"`)
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	ds, err := iDatastore.NewPostgresDatastore(ctx, "example_user", "localhost", "example_db", &iDatastore.PostgresConnectionConfig{Pass: "example_password"})
	must(err)
	defer ds.Close()

	mAdmin, err := admin.NewMessageAdmin(ds, &admin.MessageAdminConfig{AllowDestroy: true})
	must(err)
	must(mAdmin.RegisterSystem(ctx, nil))

	name := fmt.Sprintf("compat.lab.%d", time.Now().UnixNano())
	_, err = mAdmin.RegisterTopic(ctx, name, topic.SchemaVersion(1), nil)
	must(err)

	switch *expect {
	case "round-trip":
		roundTrip(ctx, ds, mAdmin, name)
	case "refused":
		refused(ctx, ds, name)
	default:
		fmt.Fprintf(os.Stderr, "-expect must be \"round-trip\" or \"refused\", got %q\n", *expect)
		os.Exit(1)
	}
}

// roundTrip is the additive verdict: the pinned build must live a full
// lifecycle against the newer database.
func roundTrip(ctx context.Context, ds *iDatastore.PostgresDatastore, mAdmin *admin.MessageAdmin, name string) {
	section("pinned producer registers and produces")
	p, err := producer.NewProducer[event](ds, nil)
	must(err)
	pInstance, err := p.Register(ctx, name, topic.SchemaVersion(1))
	check(err == nil, "producer Register accepted", err)
	for i := 1; i <= messageCount; i++ {
		_, err = pInstance.Produce(ctx, &event{Sequence: i}, producer.ProduceOptions{})
		must(err)
	}
	fmt.Printf("  ✓ produced %d messages\n", messageCount)

	section("pinned consumer registers and consumes them back")
	c, err := consumer.NewConsumer[event](ds, nil)
	must(err)
	cInstance, err := c.Register(ctx, "compat.lab.group", name, topic.SchemaVersion(1), nil)
	check(err == nil, "consumer Register accepted", err)

	consumeCtx, stop := context.WithCancel(ctx)
	defer stop()
	var consumed atomic.Int64
	err = cInstance.Consume(consumeCtx, func(ctx context.Context, e *event) error {
		if consumed.Add(1) == messageCount {
			stop()
		}
		return nil
	})
	check(consumed.Load() == messageCount, fmt.Sprintf("consumed all %d messages", messageCount), err)

	section("pinned admin reads and destroys the topic")
	row, err := mAdmin.GetTopic(ctx, name, topic.SchemaVersion(1))
	check(err == nil && row != nil, "GetTopic returns the row", err)
	must(mAdmin.DestroyTopic(ctx, name, topic.SchemaVersion(1), admin.DestroyOptions{Force: true}))
	fmt.Println("  ✓ topic destroyed")

	fmt.Println("\n✅ COMPAT LAB PASSED (round-trip)")
	fmt.Println("   The pinned build lives a full lifecycle against the migrated database.")
}

// refused is the breaking verdict: the gate must lock the pinned build out.
// No cleanup -- a checkpoint runs against a throwaway database.
func refused(ctx context.Context, ds *iDatastore.PostgresDatastore, name string) {
	section("pinned producer Register must be refused")
	p, err := producer.NewProducer[event](ds, nil)
	must(err)
	_, err = p.Register(ctx, name, topic.SchemaVersion(1))
	fmt.Printf("  error: %v\n", err)
	check(errors.Is(err, migrate.ErrSchemaNewerThanBuild), "refused with ErrSchemaNewerThanBuild", err)

	fmt.Println("\n✅ COMPAT LAB PASSED (refused)")
	fmt.Println("   The gate locks the pinned build out, as the registry declares it must.")
}

func section(title string) { fmt.Printf("\n--- %s ---\n", title) }

func check(cond bool, msg string, err error) {
	if !cond {
		fmt.Printf("  ✗ %s (error: %v)\n", msg, err)
		os.Exit(1)
	}
	fmt.Printf("  ✓ %s\n", msg)
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
