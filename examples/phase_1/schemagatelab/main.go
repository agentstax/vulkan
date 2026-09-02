package main

// schema gate lab: a producer/consumer refuses to Register against a database
// whose schema requires a newer binary -- fail fast, with a message an
// operator can act on, instead of running against a shape it can't. The gate
// allows min_compatible_version <= build <= current: a database migrated PAST
// the binary by additive steps stays usable (the rolling-deploy window); a
// step declaring MinCompatibleVersion above the build locks it out.
//
// Proves:
//  1. Register succeeds at the supported schema (v1).
//  2. additive skew: schema ahead of the binary with no breaking step ->
//     Register still succeeds.
//  3. a breaking step past the binary (system scope) refuses Register
//     (upgrade the binary).
//  4. a breaking step past ONE topic refuses that topic only -- a sibling
//     topic still registers (per-topic skew).

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/migrate"
	migratecontroller "github.com/agentstax/vulkan/pkg/migrate/controller"
	vulkan "github.com/agentstax/vulkan/pkg/vulkan"
	"github.com/jackc/pgx/v5/pgxpool"
)

type event struct{ V int }

func main() {
	if err := run(); err != nil {
		fmt.Printf("\n❌ LAB FAILED: %s\n", err.Error())
		os.Exit(1)
	}
}

// labFailure is what die panics with; run recovers it into its error so
// main's deferred cleanup runs on a failed assertion.
type labFailure struct {
	message string
}

func (f labFailure) Error() string {
	return f.message
}

func run() (err error) {
	defer func() {
		switch recovered := recover().(type) {
		case nil:
		case labFailure:
			err = recovered
		default:
			panic(recovered)
		}
	}()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pool, err := iDatastore.NewPostgresPool(ctx, "example_user", "example_password", "localhost", "example_db", nil)
	must(err)
	defer pool.Close()

	ds, err := iDatastore.NewPostgresDatastore(ctx, pool, nil)
	must(err)

	client, err := vulkan.NewClient(ds, &vulkan.ClientConfig{AllowDestroy: true})
	must(err)

	name := fmt.Sprintf("schemagate.lab.%d", time.Now().UnixNano())
	siblingName := name + ".sibling"
	topicRow, err := client.RegisterTopic(ctx, name, nil)
	must(err)
	_, err = client.RegisterTopic(ctx, siblingName, nil)
	must(err)

	controller, err := migratecontroller.NewController(ds, nil)
	must(err)
	sysOwner, err := controller.SystemOwner(ctx)
	must(err)
	defer func() {
		must(client.Topic(name).Destroy(ctx, &vulkan.DestroyOptions{Force: true}))
		must(client.Topic(siblingName).Destroy(ctx, &vulkan.DestroyOptions{Force: true}))
	}()

	// 1. supported schema -> Register succeeds -----------------------------------
	section("producer Register succeeds at the supported schema (v1)")
	_, err = client.RegisterProducer[event](ctx, name, nil)
	check(err == nil, "Register accepted at v1")

	// 2. additive skew: schema ahead, nothing breaking -> Register succeeds ------
	section("system schema ahead by an additive step -> Register still succeeds")
	bump(ctx, pool, ds.Schema, sysOwner, 2, 0)
	_, err = client.RegisterProducer[event](ctx, name, nil)
	check(err == nil, "Register accepted at v2 with no breaking step -- the rolling-deploy window")
	unbump(ctx, pool, ds.Schema, sysOwner, 2)

	// 3. breaking step past the binary (system) -> Register refused --------------
	section("system schema ahead by a breaking step -> Register refused")
	bump(ctx, pool, ds.Schema, sysOwner, 2, 2)
	_, err = client.RegisterProducer[event](ctx, name, nil)
	show(err)
	check(errors.Is(err, migrate.ErrSchemaNewerThanBuild) && strings.Contains(err.Error(), "kind system, version 2") && strings.Contains(err.Error(), "min_compatible_version 2") && strings.Contains(err.Error(), "upgrade the binary"),
		"refused, naming the system version, the requirement, and the fix")
	unbump(ctx, pool, ds.Schema, sysOwner, 2)

	// 4. breaking step past ONE topic -> that topic refused, sibling accepted ----
	section("breaking step past one topic -> that topic refused, sibling accepted")
	topicOwner := mustOwner(common.NewTopicOwner(topicRow.SystemId, topicRow.Id, topicRow.Name))
	bump(ctx, pool, ds.Schema, topicOwner, 2, 2)
	_, err = client.RegisterProducer[event](ctx, name, nil)
	show(err)
	check(errors.Is(err, migrate.ErrSchemaNewerThanBuild) && strings.Contains(err.Error(), "kind topic, version 2") && strings.Contains(err.Error(), "min_compatible_version 2"),
		"refused, naming the topic version and the requirement")
	_, err = client.RegisterProducer[event](ctx, siblingName, nil)
	check(err == nil, "sibling topic still registers -- each family gates on its own rows")
	unbump(ctx, pool, ds.Schema, topicOwner, 2)

	fmt.Println("\n✅ SCHEMA GATE LAB PASSED")
	fmt.Println("   Register rides out additive skew and fails fast, legibly, on a breaking step past the build.")
	return nil
}

// bump records a success at ver, so the gate reads that scope as version ver
// without any matching schema change -- a database a newer binary migrated.
// minCompatibleVersion 0 forges an additive step, ver forges a breaking one.
func bump(ctx context.Context, pool *pgxpool.Pool, schema string, owner *common.Owner, ver int64, minCompatibleVersion int64) {
	_, err := pool.Exec(ctx, fmt.Sprintf(`INSERT INTO %s.migration_log (system_id, topic_id, consumer_group_id, migration_version, min_compatible_version, status) VALUES ($1, $2, $3, $4, $5, 'success');`, schema), owner.SystemIdColumn(), owner.TopicIdColumn(), owner.ConsumerGroupIdColumn(), ver, minCompatibleVersion)
	must(err)
}

func unbump(ctx context.Context, pool *pgxpool.Pool, schema string, owner *common.Owner, ver int64) {
	_, err := pool.Exec(ctx, fmt.Sprintf(`DELETE FROM %s.migration_log WHERE system_id IS NOT DISTINCT FROM $1 AND topic_id IS NOT DISTINCT FROM $2 AND consumer_group_id IS NOT DISTINCT FROM $3 AND migration_version = $4;`, schema), owner.SystemIdColumn(), owner.TopicIdColumn(), owner.ConsumerGroupIdColumn(), ver)
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
		die(err.Error())
	}
}

func die(msg string) {
	panic(labFailure{message: msg})
}

func mustOwner(o *common.Owner, err error) *common.Owner { must(err); return o }

func (event) SchemaVersion() int { return 1 }
