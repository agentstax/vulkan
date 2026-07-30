package main

// entity lab: every topic row is owned by exactly one entity row (its
// lifecycle root), and destroy runs THROUGH the entity -- deleteTopic deletes
// only the entity row, so the topic row disappearing afterward is the
// ON DELETE CASCADE proven, not assumed.
//
// Confirms:
//  1. RegisterSystem enrolls the system topics -- __system.metrics owns an
//     entity typed 'topic'.
//  2. Register creates one entity row typed 'topic', stamped into
//     Topic.EntityId.
//  3. Re-registering the same config is idempotent -- same entity, no new
//     entity rows.
//  4. A second SchemaVersion under the same name is its own topic row and
//     gets its own entity.
//  5. Rename touches no entity row.
//  6. Destroy removes the entity row AND (via cascade) the topic row.
//  7. Standing orphan scan: zero entity rows unowned by any enrolled table --
//     the "no second delete path" rule made executable.

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/agentstax/vulkan/pkg/admin"
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

	step("system-topic registration enrolls")
	sysTopic, err := mAdmin.GetTopic(ctx, "__system.metrics", topic.SchemaVersion(1))
	must(err)
	if sysTopic == nil {
		die("__system.metrics is not registered")
	}
	assertEntityType(ctx, ds, sysTopic.EntityId, "topic")
	fmt.Printf("  ✓ __system.metrics owns entity %d typed 'topic'\n", sysTopic.EntityId)

	name := fmt.Sprintf("entitylab.%d", time.Now().UnixNano())

	step("register creates one entity row typed 'topic'")
	v1, err := mAdmin.RegisterTopic(ctx, name, topic.SchemaVersion(1), nil)
	must(err)
	if v1.EntityId == 0 {
		die("EntityId not populated on register")
	}
	assertEntityType(ctx, ds, v1.EntityId, "topic")
	fmt.Printf("  ✓ topic %d owns entity %d typed 'topic'\n", v1.Id, v1.EntityId)

	step("re-register is idempotent -- same entity, no new entity rows")
	before := entityCount(ctx, ds)
	again, err := mAdmin.RegisterTopic(ctx, name, topic.SchemaVersion(1), nil)
	must(err)
	if again.EntityId != v1.EntityId {
		die(fmt.Sprintf("re-register resolved entity %d, want %d", again.EntityId, v1.EntityId))
	}
	if after := entityCount(ctx, ds); after != before {
		die(fmt.Sprintf("re-register changed entity count %d -> %d", before, after))
	}
	fmt.Printf("  ✓ re-register kept entity %d, entity count unchanged\n", again.EntityId)

	step("each schema version row gets its own entity")
	v2, err := mAdmin.RegisterTopic(ctx, name, topic.SchemaVersion(2), nil)
	must(err)
	if v2.EntityId == v1.EntityId {
		die(fmt.Sprintf("version 2 shares version 1's entity %d", v1.EntityId))
	}
	assertEntityType(ctx, ds, v2.EntityId, "topic")
	fmt.Printf("  ✓ version 2 (topic %d) owns its own entity %d\n", v2.Id, v2.EntityId)

	step("rename touches no entity row")
	before = entityCount(ctx, ds)
	newName := name + ".renamed"
	renamed, err := mAdmin.RenameTopic(ctx, name, newName)
	must(err)
	for _, r := range renamed {
		if r.EntityId != v1.EntityId && r.EntityId != v2.EntityId {
			die(fmt.Sprintf("rename changed an entity id: got %d, want %d or %d", r.EntityId, v1.EntityId, v2.EntityId))
		}
	}
	if after := entityCount(ctx, ds); after != before {
		die(fmt.Sprintf("rename changed entity count %d -> %d", before, after))
	}
	fmt.Printf("  ✓ %d versions renamed, entity ids and count unchanged\n", len(renamed))

	step("destroy removes entity AND topic row (cascade direction proven)")
	must(mAdmin.DestroyTopic(ctx, newName, topic.SchemaVersion(1), admin.DestroyOptions{Force: true}))
	assertGone(ctx, ds, "entity", v1.EntityId)
	assertGone(ctx, ds, "topic", v1.Id)
	if still, err := mAdmin.GetTopic(ctx, newName, topic.SchemaVersion(1)); err != nil || still != nil {
		die(fmt.Sprintf("destroyed topic still resolves: topic=%v err=%v", still, err))
	}
	fmt.Printf("  ✓ entity %d deleted, topic row %d cascaded away\n", v1.EntityId, v1.Id)

	// version 2 must be untouched by version 1's destroy
	assertEntityType(ctx, ds, v2.EntityId, "topic")
	must(mAdmin.DestroyTopic(ctx, newName, topic.SchemaVersion(2), admin.DestroyOptions{Force: true}))
	assertGone(ctx, ds, "entity", v2.EntityId)
	assertGone(ctx, ds, "topic", v2.Id)
	fmt.Printf("  ✓ version 2 survived version 1's destroy, then destroyed clean\n")

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

	fmt.Printf("\n✅ entity lab PASSED\n")
}

// ---- helpers ----

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

func assertGone(ctx context.Context, ds *coredatastore.PostgresDatastore, table string, id int64) {
	var count int
	must(ds.Pool.QueryRow(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE id = $1;`, table), id).Scan(&count))
	if count != 0 {
		die(fmt.Sprintf("%s row %d still exists after destroy", table, id))
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
