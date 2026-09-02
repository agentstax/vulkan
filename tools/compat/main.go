package main

// compat lab: the PINNED release's side of the cross-version compatibility
// matrix. This module's vulkan dependency is whatever go.mod pins -- the
// prior release at a checkpoint, the working tree in dry-runs -- while the
// database it runs against was migrated by the working tree. It drives ONLY
// the public API: the compatibility surface is the public surface.
//
// The schema is the lab's one cross-build hazard: a pinned build predating
// PostgresConnectionConfig.Schema has no such field and resolves its tables
// through the connection's own search_path, which is public. So at a release
// checkpoint the working tree must migrate public too -- see the pin flow in
// go.mod. requireMigratedSchema is what stops a mismatch from passing: without
// it the pinned build would create a second, empty set of tables in whatever
// schema it landed in and round-trip against itself, comparing nothing.
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
	vulkan "github.com/agentstax/vulkan/pkg/vulkan"
)

const messageCount = 5

type event struct{ Sequence int }

func (event) SchemaVersion() int { return 1 }

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// requireMigratedSchema fails unless the schema THIS build resolves to is the
// one the working tree migrated. An unmigrated schema is the tell that the two
// builds resolved to different ones -- without this the pinned build would
// create its own empty set and round-trip against itself.
func requireMigratedSchema(ctx context.Context, ds *iDatastore.PostgresDatastore) error {
	var applied int
	sql := fmt.Sprintf(`SELECT count(*) FROM %s.migration_log WHERE status = 'success';`, ds.Schema)
	err := ds.Pool.QueryRow(ctx, sql).Scan(&applied)
	if err != nil {
		return fmt.Errorf("this build resolves to schema %q, which holds no migration_log, so it would compare against tables it created itself -- "+
			"migrate the working tree into that schema: %w", ds.Schema, err)
	}
	if applied == 0 {
		return fmt.Errorf("this build resolves to schema %q, which has no applied migration steps, so it would compare against tables it created itself -- "+
			"migrate the working tree into that schema", ds.Schema)
	}
	return nil
}

func run() error {
	expect := flag.String("expect", "round-trip", `declared verdict: "round-trip" | "refused"`)
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool, err := vulkan.NewPostgresPool(ctx, "example_user", "example_password", "localhost", "example_db", nil)
	if err != nil {
		return err
	}
	defer pool.Close()

	ds, err := iDatastore.NewPostgresDatastore(ctx, pool, nil)
	if err != nil {
		return err
	}

	if err := requireMigratedSchema(ctx, ds); err != nil {
		return err
	}

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
	p, err := producer.NewProducer(ds, nil)
	if err != nil {
		return err
	}
	pInstance, err := p.Register[event](ctx, name)
	if problem := check(err == nil, "producer Register accepted", err); problem != nil {
		return problem
	}
	for i := 1; i <= messageCount; i++ {
		if _, err := pInstance.Produce(ctx, &event{Sequence: i}, nil); err != nil {
			return fmt.Errorf("message %d: %w", i, err)
		}
	}
	fmt.Printf("  ✓ produced %d messages\n", messageCount)

	section("pinned consumer registers and consumes them back")
	c, err := consumer.NewConsumer(ds, nil)
	if err != nil {
		return err
	}
	cInstance, err := c.Register[event](ctx, "compat.lab.group", name, nil)
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
	}, nil)
	if problem := check(consumed.Load() == messageCount, fmt.Sprintf("consumed all %d messages", messageCount), err); problem != nil {
		return problem
	}

	section("pinned admin reads and destroys the topic")
	row, err := mAdmin.GetTopic(ctx, name)
	if problem := check(err == nil && row != nil, "GetTopic returns the row", err); problem != nil {
		return problem
	}
	if err := mAdmin.DestroyTopic(ctx, name, &admin.DestroyOptions{Force: true}); err != nil {
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
	p, err := producer.NewProducer(ds, nil)
	if err != nil {
		return err
	}
	_, err = p.Register[event](ctx, name)
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
