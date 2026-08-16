package main

// consumer group registry lab: a group is a resource owned by exactly one
// topic -- one registry row whose topic_id FK CASCADE is its whole lifecycle.
// Group + cursor are created in ONE txn; destroying the topic (or deleting
// the group row) cascades everything, proven, not assumed.
//
// Confirms:
//  1. RegisterGroup registers the group with its cursor in one txn, and
//     GetGroup resolves it by (topic, name).
//  2. the same name on a SECOND topic is a DIFFERENT group -- own registry
//     row. Names are scoped per topic, not global.
//  3. N concurrent first-registrations leave exactly one registry row --
//     the advisory-lock shape under real contention.
//  4. destroying a topic destroys ITS groups (registry row, cursor)
//     and leaves the same-named group on the other topic untouched.
//  5. deleting a group row directly cascades its cursor away -- DestroyGroup
//     is this delete plus the rows no FK reaches.
//  6. DestroyGroup: AllowDestroy-gated, not-found error, refused while the
//     group has a live worker instance or delivery rows, and force sweeps
//     every row the group owns.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/agentstax/vulkan/pkg/admin"
	"github.com/agentstax/vulkan/pkg/common"
	consumercontroller "github.com/agentstax/vulkan/pkg/consumer/controller"
	coredatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/topic"
	workercontroller "github.com/agentstax/vulkan/pkg/worker/controller"
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

	cd, err := consumercontroller.NewConsumerController(ds, nil)
	must(err)

	suffix := time.Now().UnixNano()
	topicA, err := mAdmin.RegisterTopic(ctx, fmt.Sprintf("consumergrouplab.a.%d", suffix), topic.SchemaVersion(1), nil)
	must(err)
	topicB, err := mAdmin.RegisterTopic(ctx, fmt.Sprintf("consumergrouplab.b.%d", suffix), topic.SchemaVersion(1), nil)
	must(err)

	step("RegisterGroup registers the group with its children in one txn")
	group := fmt.Sprintf("consumergrouplab.group.%d", suffix)
	registered, err := cd.RegisterGroup(ctx, topicA.Id, group)
	must(err)
	g, err := cd.GetGroup(ctx, topicA.Id, group)
	must(err)
	if g == nil || g.Id != registered.Id || g.TopicId != topicA.Id || g.CreatedAt.IsZero() {
		die(fmt.Sprintf("GetGroup returned %+v, want id %d on topic %d with created_at set", g, registered.Id, topicA.Id))
	}
	assertChildren(ctx, ds, registered.Id, 1, "at registration")
	fmt.Printf("  ✓ group %q (id %d) on topic %d, cursor created with it\n", group, registered.Id, topicA.Id)

	step("same name on a second topic is a DIFFERENT group")
	other, err := cd.RegisterGroup(ctx, topicB.Id, group)
	must(err)
	if other.Id == registered.Id {
		die(fmt.Sprintf("second topic reused the first topic's group: %+v", other))
	}
	fmt.Printf("  ✓ own registry row (id %d vs %d)\n", other.Id, registered.Id)

	step("concurrent first-registrations leave exactly one registry row")
	race := fmt.Sprintf("consumergrouplab.race.%d", suffix)
	var wg sync.WaitGroup
	errs := make(chan error, 10)
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := cd.RegisterGroup(ctx, topicA.Id, race)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		must(err)
	}
	var raceRows int
	must(ds.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM consumer_group WHERE topic_id = $1 AND name = $2;`, topicA.Id, race).Scan(&raceRows))
	if raceRows != 1 {
		die(fmt.Sprintf("race group has %d registry rows, want 1", raceRows))
	}
	fmt.Printf("  ✓ 10 concurrent registrations -> one registry row\n")

	step("destroying a topic destroys ITS groups and no one else's")
	must(mAdmin.DestroyTopic(ctx, topicB.Name, topic.SchemaVersion(1), admin.DestroyOptions{Force: true}))
	var bRows int
	must(ds.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM consumer_group WHERE id = $1;`, other.Id).Scan(&bRows))
	if bRows != 0 {
		die("topicB's group survived its topic's Destroy")
	}
	assertChildren(ctx, ds, other.Id, 0, "after topicB's destroy")
	if g, err := cd.GetGroup(ctx, topicA.Id, group); err != nil || g == nil || g.Id != registered.Id {
		die(fmt.Sprintf("topic destroy touched the OTHER topic's group: %+v err=%v", g, err))
	}
	assertChildren(ctx, ds, registered.Id, 1, "after topicB's destroy")
	fmt.Printf("  ✓ topicB's group + children cascaded away, topicA's same-named group untouched\n")

	step("deleting a group row cascades its cursor")
	if _, err := ds.Pool.Exec(ctx, `DELETE FROM consumer_group WHERE id = $1;`, registered.Id); err != nil {
		die(err.Error())
	}
	gone, err := cd.GetGroup(ctx, topicA.Id, group)
	must(err)
	if gone != nil {
		die(fmt.Sprintf("GetGroup still resolves the deleted group: %+v", gone))
	}
	assertChildren(ctx, ds, registered.Id, 0, "after the group row's delete")
	fmt.Printf("  ✓ group %d deleted, cursor cascaded away\n", registered.Id)

	destroySection(ctx, ds, mAdmin, cd, topicA, suffix)

	// cleanup
	_, err = ds.Pool.Exec(ctx, `DELETE FROM consumer_group WHERE topic_id = $1 AND name = $2;`, topicA.Id, race)
	must(err)
	must(mAdmin.DestroyTopic(ctx, topicA.Name, topic.SchemaVersion(1), admin.DestroyOptions{Force: true}))

	fmt.Printf("\n✅ consumer group registry lab PASSED\n")
}

func destroySection(ctx context.Context, ds *coredatastore.PostgresDatastore, mAdmin *admin.MessageAdmin, cd *consumercontroller.ConsumerController, topicA *topic.Topic, suffix int64) {
	step("DestroyGroup: gate + not-found, live/backlogged guards, force sweeps everything")

	doomedName := fmt.Sprintf("consumergrouplab.doomed.%d", suffix)
	doomed, err := cd.RegisterGroup(ctx, topicA.Id, doomedName)
	must(err)
	_, err = cd.DeclareBindings(ctx, doomed.Id, []string{"some.routing.key"}, time.Now())
	must(err)

	locked, err := admin.NewMessageAdmin(ds, nil)
	must(err)
	if err := locked.DestroyGroup(ctx, topicA.Name, topic.SchemaVersion(1), doomedName, admin.DestroyOptions{}); !errors.Is(err, admin.ErrDestroyDisabled) {
		die(fmt.Sprintf("destroy without AllowDestroy: want ErrDestroyDisabled, got %v", err))
	}
	if err := mAdmin.DestroyGroup(ctx, topicA.Name, topic.SchemaVersion(1), doomedName+".missing", admin.DestroyOptions{}); !errors.Is(err, consumercontroller.ErrGroupNotFound) {
		die(fmt.Sprintf("destroy of an unregistered group: want ErrGroupNotFound, got %v", err))
	}
	fmt.Printf("  ✓ AllowDestroy gate and not-found error\n")

	// a live worker instance -- what a running consumer heartbeats -- refuses
	// the destroy; releasing it clears the guard
	workers, err := workercontroller.NewWorkerController(ds, nil)
	must(err)
	groupOwner, err := common.NewConsumerGroupOwner(topicA.SystemId, topicA.Id, doomed.Id, doomedName)
	must(err)
	must(workers.RegisterWorker(ctx, "message_consumer", groupOwner, nil))
	row, err := workers.GetWorker(ctx, "message_consumer", groupOwner)
	must(err)
	claimed, err := workers.ClaimInstance(ctx, row.Id, 30*time.Second)
	must(err)
	if claimed == nil {
		die("the lab's own worker claim was declined")
	}
	if err := mAdmin.DestroyGroup(ctx, topicA.Name, topic.SchemaVersion(1), doomedName, admin.DestroyOptions{}); !errors.Is(err, consumercontroller.ErrGroupLive) {
		die(fmt.Sprintf("destroy with a live worker instance: want ErrGroupLive, got %v", err))
	}
	must(workers.ReleaseInstance(ctx, claimed.Id, claimed.Token))
	fmt.Printf("  ✓ live worker instance refuses the destroy\n")

	// delivery rows refuse it; force discards them along with the rows no FK
	// reaches (lease, key_lease, delivery_log)
	_, err = ds.Pool.Exec(ctx, fmt.Sprintf(`INSERT INTO delivery_%d (consumer_group_id, message_id, status) VALUES ($1, 1, 'ready');`, topicA.Id), doomed.Id)
	must(err)
	_, err = ds.Pool.Exec(ctx, `INSERT INTO lease (consumer_group_id, low, high, until) VALUES ($1, 1, 10, now() + interval '1 minute');`, doomed.Id)
	must(err)
	_, err = ds.Pool.Exec(ctx, fmt.Sprintf(`INSERT INTO delivery_log_%d (consumer_group_id, message_id, attempt, status, error) VALUES ($1, 1, 1, 'failure', 'lab');`, topicA.Id), doomed.Id)
	must(err)
	_, err = ds.Pool.Exec(ctx, `INSERT INTO key_lease (consumer_group_id, compaction_key, lease_token, expires_at) VALUES ($1, 'labkey', gen_random_uuid(), now());`, doomed.Id)
	must(err)
	if err := mAdmin.DestroyGroup(ctx, topicA.Name, topic.SchemaVersion(1), doomedName, admin.DestroyOptions{}); !errors.Is(err, consumercontroller.ErrGroupDeliveriesPending) {
		die(fmt.Sprintf("destroy with delivery rows: want ErrGroupDeliveriesPending, got %v", err))
	}
	must(mAdmin.DestroyGroup(ctx, topicA.Name, topic.SchemaVersion(1), doomedName, admin.DestroyOptions{Force: true}))

	for what, sql := range map[string]string{
		"group rows":        `SELECT COUNT(*) FROM consumer_group WHERE id = $1;`,
		"cursor rows":       `SELECT COUNT(*) FROM cursor WHERE consumer_group_id = $1;`,
		"binding rows":      `SELECT COUNT(*) FROM binding WHERE consumer_group_id = $1;`,
		"worker rows":       `SELECT COUNT(*) FROM worker WHERE consumer_group_id = $1;`,
		"instance rows":     `SELECT COUNT(*) FROM worker_instance wi WHERE wi.worker_id IN (SELECT id FROM worker WHERE consumer_group_id = $1);`,
		"lease rows":        `SELECT COUNT(*) FROM lease WHERE consumer_group_id = $1;`,
		"key lease rows":    `SELECT COUNT(*) FROM key_lease WHERE consumer_group_id = $1;`,
		"delivery rows":     fmt.Sprintf(`SELECT COUNT(*) FROM delivery_%d WHERE consumer_group_id = $1;`, topicA.Id),
		"delivery log rows": fmt.Sprintf(`SELECT COUNT(*) FROM delivery_log_%d WHERE consumer_group_id = $1;`, topicA.Id),
	} {
		var count int
		must(ds.Pool.QueryRow(ctx, sql, doomed.Id).Scan(&count))
		if count != 0 {
			die(fmt.Sprintf("force destroy left %d %s behind", count, what))
		}
	}
	fmt.Printf("  ✓ delivery backlog refused, force swept group/cursor/binding/worker/instances/leases/deliveries\n")
}

// ---- helpers ----

// assertChildren counts the group's cursor row -- it exists and dies
// together with the registry row (want 1 or 0).
func assertChildren(ctx context.Context, ds *coredatastore.PostgresDatastore, groupID int64, want int, when string) {
	var cursors int
	must(ds.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM cursor WHERE consumer_group_id = $1;`, groupID).Scan(&cursors))
	if cursors != want {
		die(fmt.Sprintf("group %d has %d cursors %s, want %d", groupID, cursors, when, want))
	}
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
