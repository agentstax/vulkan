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

func (event) SchemaVersion() topic.SchemaVersion { return 1 }

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	expect := flag.String("expect", "round-trip", `declared verdict: "round-trip" | "refused"`)
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	ds, err := iDatastore.NewPostgresDatastore(ctx, "example_user", "localhost", "example_db", &iDatastore.PostgresConnectionConfig{Pass: "example_password"})
	if err != nil {
		return err
	}
	defer ds.Close()

	mAdmin, err := admin.NewMessageAdmin(ds, &admin.MessageAdminConfig{AllowDestroy: true})
	if err != nil {
		return err
	}
	if err := mAdmin.RegisterSystem(ctx, nil); err != nil {
		return err
	}

	name := fmt.Sprintf("compat.lab.%d", time.Now().UnixNano())
	if _, err := mAdmin.RegisterTopic(ctx, name, nil); err != nil {
		return err
	}

	switch *expect {
	case "round-trip":
		return roundTrip(ctx, ds, mAdmin, name)
	case "refused":
		return refused(ctx, ds, name)
	}
	return fmt.Errorf("-expect must be \"round-trip\" or \"refused\", got %q", *expect)
}

// roundTrip is the additive verdict: the pinned build must live a full
// lifecycle against the newer database.
func roundTrip(ctx context.Context, ds *iDatastore.PostgresDatastore, mAdmin *admin.MessageAdmin, name string) error {
	section("pinned producer registers and produces")
	p, err := producer.NewProducer[event](ds, nil)
	if err != nil {
		return err
	}
	pInstance, err := p.Register(ctx, name)
	if problem := check(err == nil, "producer Register accepted", err); problem != nil {
		return problem
	}
	for i := 1; i <= messageCount; i++ {
		if _, err := pInstance.Produce(ctx, &event{Sequence: i}, producer.ProduceOptions{}); err != nil {
			return fmt.Errorf("message %d: %w", i, err)
		}
	}
	fmt.Printf("  ✓ produced %d messages\n", messageCount)

	section("pinned consumer registers and consumes them back")
	c, err := consumer.NewConsumer[event](ds, nil)
	if err != nil {
		return err
	}
	cInstance, err := c.Register(ctx, "compat.lab.group", name, nil)
	if problem := check(err == nil, "consumer Register accepted", err); problem != nil {
		return problem
	}

	consumeCtx, stop := context.WithCancel(ctx)
	defer stop()
	var consumed atomic.Int64
	err = cInstance.Consume(consumeCtx, func(ctx context.Context, e *event) error {
		if consumed.Add(1) == messageCount {
			stop()
		}
		return nil
	})
	if problem := check(consumed.Load() == messageCount, fmt.Sprintf("consumed all %d messages", messageCount), err); problem != nil {
		return problem
	}

	section("pinned admin reads and destroys the topic")
	row, err := mAdmin.GetTopic(ctx, name)
	if problem := check(err == nil && row != nil, "GetTopic returns the row", err); problem != nil {
		return problem
	}
	if err := mAdmin.DestroyTopic(ctx, name, admin.DestroyOptions{Force: true}); err != nil {
		return err
	}
	fmt.Println("  ✓ topic destroyed")

	fmt.Println("\n✅ COMPAT LAB PASSED (round-trip)")
	fmt.Println("   The pinned build lives a full lifecycle against the migrated database.")
	return nil
}

// refused is the breaking verdict: the gate must lock the pinned build out.
// No cleanup -- a checkpoint runs against a throwaway database.
func refused(ctx context.Context, ds *iDatastore.PostgresDatastore, name string) error {
	section("pinned producer Register must be refused")
	p, err := producer.NewProducer[event](ds, nil)
	if err != nil {
		return err
	}
	_, err = p.Register(ctx, name)
	fmt.Printf("  error: %v\n", err)
	if problem := check(errors.Is(err, migrate.ErrSchemaNewerThanBuild), "refused with ErrSchemaNewerThanBuild", err); problem != nil {
		return problem
	}

	fmt.Println("\n✅ COMPAT LAB PASSED (refused)")
	fmt.Println("   The gate locks the pinned build out, as the registry declares it must.")
	return nil
}

func section(title string) { fmt.Printf("\n--- %s ---\n", title) }

// check prints the claim when it holds and returns it as an error when it does
// not. err is the value the claim was made about -- nil when the claim is
// about something else, so it is reported only when there is one.
func check(cond bool, claim string, err error) error {
	if !cond {
		if err != nil {
			return fmt.Errorf("claim did not hold -- %s: %w", claim, err)
		}
		return errors.New("claim did not hold -- " + claim)
	}
	fmt.Printf("  ✓ %s\n", claim)
	return nil
}
