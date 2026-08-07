package main

// consumer group registry lab: a group is a resource owned by exactly one
// topic -- one registry row whose topic_id FK CASCADE is its whole lifecycle.
// Group + cursor + waterline duty are created in ONE txn; destroying the
// topic (or deleting the group row) cascades everything, proven, not assumed.
//
// Confirms:
//  1. RegisterGroup registers the group with its cursor and waterline duty in
//     one txn, and GetGroup resolves it by (topic, name).
//  2. the same name on a SECOND topic is a DIFFERENT group -- own registry
//     row. Names are scoped per topic, not global.
//  3. N concurrent first-registrations leave exactly one registry row --
//     the advisory-lock shape under real contention.
//  4. destroying a topic destroys ITS groups (registry row, cursor, duty)
//     and leaves the same-named group on the other topic untouched.
//  5. deleting a group row directly cascades its cursor and waterline duty
//     away -- the future group-Destroy verb is exactly this delete.

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/agentstax/vulkan/pkg/admin"
	consumercontroller "github.com/agentstax/vulkan/pkg/consumer/controller"
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
	fmt.Printf("  ✓ group %q (id %d) on topic %d, cursor + waterline duty created with it\n", group, registered.Id, topicA.Id)

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

	step("deleting a group row cascades its cursor and waterline duty")
	if _, err := ds.Pool.Exec(ctx, `DELETE FROM consumer_group WHERE id = $1;`, registered.Id); err != nil {
		die(err.Error())
	}
	gone, err := cd.GetGroup(ctx, topicA.Id, group)
	must(err)
	if gone != nil {
		die(fmt.Sprintf("GetGroup still resolves the deleted group: %+v", gone))
	}
	assertChildren(ctx, ds, registered.Id, 0, "after the group row's delete")
	fmt.Printf("  ✓ group %d deleted, cursor AND waterline duty cascaded away\n", registered.Id)

	// cleanup
	_, err = ds.Pool.Exec(ctx, `DELETE FROM consumer_group WHERE topic_id = $1 AND name = $2;`, topicA.Id, race)
	must(err)
	must(mAdmin.DestroyTopic(ctx, topicA.Name, topic.SchemaVersion(1), admin.DestroyOptions{Force: true}))

	fmt.Printf("\n✅ consumer group registry lab PASSED\n")
}

// ---- helpers ----

// assertChildren counts the group's cursor row and waterline duty row -- they
// exist and die together with the registry row (want 1 or 0 of each).
func assertChildren(ctx context.Context, ds *coredatastore.PostgresDatastore, groupID int64, want int, when string) {
	var cursors, duties int
	must(ds.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM cursor WHERE consumer_group_id = $1;`, groupID).Scan(&cursors))
	must(ds.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM maintenance WHERE duty = 'waterline' AND consumer_group_id = $1;`, groupID).Scan(&duties))
	if cursors != want || duties != want {
		die(fmt.Sprintf("group %d has %d cursors and %d waterline duties %s, want %d of each", groupID, cursors, duties, when, want))
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
