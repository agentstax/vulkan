package main

// consumer group registry lab: a group is the RESOURCE (one registry row, one
// entity), its cursor + waterline duty per-topic children -- UpsertGroup
// creates group and children in ONE txn. Destroy runs THROUGH the entity:
// deleting the entity row removes the registry row AND (via the cursor FK) the
// group's cursors, cascade proven, not assumed.
//
// Confirms:
//  1. UpsertGroup registers the group -- one consumer_group row + one entity
//     typed 'consumer_group' -- and GetGroup resolves it.
//  2. re-upserting the group for a SECOND topic's cursor reuses the one entity.
//  3. N concurrent first-registrations leave exactly one registry row, one
//     entity, zero orphans -- the advisory-lock shape under real contention.
//  4. destroying a topic deletes the group's cursor there but does NOT touch
//     the registry.
//  5. deleting the group's entity row cascades the registry row away.
//  6. standing orphan scan across BOTH enrolled tables comes back zero.

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/agentstax/vulkan/examples/phase_1/common"
	"github.com/agentstax/vulkan/pkg/admin"
	"github.com/agentstax/vulkan/pkg/consumer"
	coredatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/topic"
)

func main() {
	ctx := context.Background()

	ds, err := coredatastore.NewPostgresDatastore(ctx, &coredatastore.PostgresConnectionConfig{
		User: "example_user", Pass: "example_password",
		Host: "localhost", Port: 5432, Database: "example_db",
	})
	must(err)
	defer ds.Close()

	mAdmin, err := admin.NewMessageAdmin(ds, &admin.MessageAdminConfig{AllowDestroy: true})
	must(err)
	must(mAdmin.RegisterSystem(ctx, nil))

	cd, err := consumer.NewConsumerDatastore[common.Work](ds, nil)
	must(err)

	suffix := time.Now().UnixNano()
	topicA, err := mAdmin.RegisterTopic(ctx, fmt.Sprintf("consumergrouplab.a.%d", suffix), topic.SchemaVersion(1), nil)
	must(err)
	topicB, err := mAdmin.RegisterTopic(ctx, fmt.Sprintf("consumergrouplab.b.%d", suffix), topic.SchemaVersion(1), nil)
	must(err)

	step("UpsertGroup registers the group")
	group := fmt.Sprintf("consumergrouplab.group.%d", suffix)
	registered, err := cd.UpsertGroup(ctx, topicA.Id, group)
	must(err)
	entityId := groupEntity(ctx, ds, group)
	if registered.EntityId != entityId {
		die(fmt.Sprintf("UpsertGroup returned entity %d, registry holds %d", registered.EntityId, entityId))
	}
	assertEntityType(ctx, ds, entityId, "consumer_group")
	g, err := cd.GetGroup(ctx, group)
	must(err)
	if g == nil || g.EntityId != entityId || g.CreatedAt.IsZero() {
		die(fmt.Sprintf("GetGroup returned %+v, want entity %d with created_at set", g, entityId))
	}
	fmt.Printf("  ✓ group %q owns entity %d typed 'consumer_group', GetGroup resolves it\n", group, entityId)

	step("same group on a second topic reuses the one entity")
	before := entityCount(ctx, ds)
	again, err := cd.UpsertGroup(ctx, topicB.Id, group)
	must(err)
	if again.EntityId != entityId {
		die(fmt.Sprintf("second topic re-registered the group: entity %d, want %d", again.EntityId, entityId))
	}
	if after := entityCount(ctx, ds); after != before {
		die(fmt.Sprintf("second topic changed entity count %d -> %d", before, after))
	}
	fmt.Printf("  ✓ entity %d reused, entity count unchanged\n", entityId)

	step("concurrent first-registrations leave exactly one entity")
	race := fmt.Sprintf("consumergrouplab.race.%d", suffix)
	var wg sync.WaitGroup
	errs := make(chan error, 10)
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := cd.UpsertGroup(ctx, topicA.Id, race)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		must(err)
	}
	var raceRows int
	must(ds.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM consumer_group WHERE name = $1;`, race).Scan(&raceRows))
	if raceRows != 1 {
		die(fmt.Sprintf("race group has %d registry rows, want 1", raceRows))
	}
	raceEntityId := groupEntity(ctx, ds, race)
	assertEntityType(ctx, ds, raceEntityId, "consumer_group")
	fmt.Printf("  ✓ 10 concurrent registrations -> one registry row, one entity %d\n", raceEntityId)

	step("destroying a topic does NOT touch the registry")
	must(mAdmin.DestroyTopic(ctx, topicB.Name, topic.SchemaVersion(1), admin.DestroyOptions{Force: true}))
	if got := groupEntity(ctx, ds, group); got != entityId {
		die(fmt.Sprintf("topic destroy changed the group's entity: %d, want %d", got, entityId))
	}
	var cursors int
	must(ds.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM cursor WHERE consumer_group_id = $1;`, registered.Id).Scan(&cursors))
	if cursors != 1 {
		die(fmt.Sprintf("group has %d cursors after topic destroy, want 1 (topicA's)", cursors))
	}
	fmt.Printf("  ✓ registry intact, only the destroyed topic's cursor is gone\n")

	step("deleting the group's entity cascades the registry row")
	if _, err := ds.Pool.Exec(ctx, `DELETE FROM entity WHERE id = $1;`, entityId); err != nil {
		die(err.Error())
	}
	var remaining int
	must(ds.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM consumer_group WHERE name = $1;`, group).Scan(&remaining))
	if remaining != 0 {
		die("registry row survived its entity's delete")
	}
	gone, err := cd.GetGroup(ctx, group)
	must(err)
	if gone != nil {
		die(fmt.Sprintf("GetGroup still resolves the destroyed group: %+v", gone))
	}
	must(ds.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM cursor WHERE consumer_group_id = $1;`, registered.Id).Scan(&cursors))
	if cursors != 0 {
		die(fmt.Sprintf("group still has %d cursors after its entity's delete -- cursor FK cascade didn't fire", cursors))
	}
	fmt.Printf("  ✓ entity %d deleted, registry row AND cursors cascaded away, GetGroup returns nil\n", entityId)

	// cleanup so the orphan scan sees a settled world
	_, err = ds.Pool.Exec(ctx, `DELETE FROM entity WHERE id = $1;`, raceEntityId)
	must(err)
	must(mAdmin.DestroyTopic(ctx, topicA.Name, topic.SchemaVersion(1), admin.DestroyOptions{Force: true}))

	step("standing orphan scan -- zero entity rows unowned by any enrolled table")
	var orphans int
	must(ds.Pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM entity e
		LEFT JOIN topic t ON t.entity_id = e.id
		LEFT JOIN consumer_group g ON g.entity_id = e.id
		WHERE t.entity_id IS NULL AND g.entity_id IS NULL;
	`).Scan(&orphans))
	if orphans != 0 {
		die(fmt.Sprintf("%d orphaned entity rows -- a register or destroy path leaked", orphans))
	}
	fmt.Printf("  ✓ zero orphaned entity rows\n")

	fmt.Printf("\n✅ consumer group registry lab PASSED\n")
}

// ---- helpers ----

// groupEntity resolves the registry row's entity_id, dying if the group isn't registered.
func groupEntity(ctx context.Context, ds *coredatastore.PostgresDatastore, group string) int64 {
	var entityId int64
	err := ds.Pool.QueryRow(ctx, `SELECT entity_id FROM consumer_group WHERE name = $1;`, group).Scan(&entityId)
	if err != nil {
		die(fmt.Sprintf("group %q: %v", group, err))
	}
	return entityId
}

func assertEntityType(ctx context.Context, ds *coredatastore.PostgresDatastore, entityId int64, wantType string) {
	var gotType string
	err := ds.Pool.QueryRow(ctx, `SELECT type FROM entity WHERE id = $1;`, entityId).Scan(&gotType)
	if err != nil {
		die(fmt.Sprintf("entity %d: %v", entityId, err))
	}
	if gotType != wantType {
		die(fmt.Sprintf("entity %d has type %q, want %q", entityId, gotType, wantType))
	}
}

func entityCount(ctx context.Context, ds *coredatastore.PostgresDatastore) int {
	var count int
	must(ds.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM entity;`).Scan(&count))
	return count
}

func step(s string) { fmt.Printf("\n--- %s ---\n", s) }
func must(err error) {
	if err != nil {
		die(err.Error())
	}
}
func die(msg string) {
	fmt.Printf("\n❌ LAB FAILED: %s\n", msg)
	os.Exit(1)
}
