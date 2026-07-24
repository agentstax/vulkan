package main

// schema gate lab: a producer/consumer refuses to Register against a database
// whose schema version is outside the range this build understands -- fail
// fast, with a message an operator can act on, instead of running against a
// shape it can't. The v1 gate mostly asserts "schema says v1"; the mechanism is
// what a v1.1 binary relies on.
//
// Proves:
//  1. Register succeeds at the supported schema (v1).
//  2. a system schema ahead of the binary refuses Register (upgrade the binary).
//  3. a topic schema ahead of the binary refuses Register (per-topic, same gate).

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/agentstax/vulkan/pkg/admin"
	coredatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/jackc/pgx/v5/pgxpool"
)

type event struct{ V int }

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ds, err := coredatastore.NewPostgresDatastore(ctx, &coredatastore.PostgresConnectionConfig{
		User: "example_user", Pass: "example_password",
		Host: "localhost", Port: 5432, Database: "example_db",
	})
	must(err)
	defer ds.Close()
	pool := ds.Pool

	mAdmin, err := admin.NewMessageAdmin(ds, &admin.MessageAdminConfig{AllowDestroy: true})
	must(err)
	must(mAdmin.RegisterSystem(ctx))

	name := fmt.Sprintf("schemagate.lab.%d", time.Now().UnixNano())
	topicRow, err := mAdmin.RegisterTopic(ctx, name, nil)
	must(err)
	defer func() { must(mAdmin.DestroyTopic(ctx, name, admin.DestroyOptions{Force: true})) }()

	// 1. supported schema -> Register succeeds -----------------------------------
	section("producer Register succeeds at the supported schema (v1)")
	check(newProducer(name, ds).Register(ctx) == nil, "Register accepted at v1")

	// 2. system schema ahead of the binary --------------------------------------
	section("system schema ahead of the binary -> Register refused")
	bump(ctx, pool, "system", 0, 2)
	err = newProducer(name, ds).Register(ctx)
	show(err)
	check(err != nil && strings.Contains(err.Error(), "system schema is version 2") && strings.Contains(err.Error(), "upgrade the binary"),
		"refused, naming the system version and the fix")
	unbump(ctx, pool, "system", 0, 2)

	// 3. topic schema ahead of the binary ---------------------------------------
	section("topic schema ahead of the binary -> Register refused")
	bump(ctx, pool, "topic", topicRow.Id, 2)
	err = newProducer(name, ds).Register(ctx)
	show(err)
	check(err != nil && strings.Contains(err.Error(), "topic schema is version 2") && strings.Contains(err.Error(), "upgrade the binary"),
		"refused, naming the topic version and the fix")
	unbump(ctx, pool, "topic", topicRow.Id, 2)

	fmt.Println("\n✅ SCHEMA GATE LAB PASSED")
	fmt.Println("   Register fails fast and legibly when the schema is outside the supported range.")
}

func newProducer(name string, ds *coredatastore.PostgresDatastore) *producer.MessageProducer[event] {
	p, err := producer.NewMessageProducer[event](name, ds, nil)
	must(err)
	return p
}

// bump records a success at ver, so the gate reads that scope as version ver
// without any matching schema change -- a database a newer binary migrated.
func bump(ctx context.Context, pool *pgxpool.Pool, entityType string, entityID, ver int64) {
	_, err := pool.Exec(ctx, `INSERT INTO schema_log (entity_type, entity_id, schema_version, status) VALUES ($1, $2, $3, 'success');`, entityType, entityID, ver)
	must(err)
}

func unbump(ctx context.Context, pool *pgxpool.Pool, entityType string, entityID, ver int64) {
	_, err := pool.Exec(ctx, `DELETE FROM schema_log WHERE entity_type = $1 AND entity_id = $2 AND schema_version = $3;`, entityType, entityID, ver)
	must(err)
}

func section(title string) { fmt.Printf("\n--- %s ---\n", title) }
func show(err error)       { fmt.Printf("  error: %v\n", err) }

func check(cond bool, msg string) {
	if !cond {
		fmt.Printf("  ✗ %s\n", msg)
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
