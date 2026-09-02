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
//  6. Start: consumergroup.Head() creates the cursor at MAX(id) of the log,
//     only a post-register produce is delivered, and a later Register with
//     another position leaves the row alone.
//  7. DestroyGroup: AllowDestroy-gated, not-found error, refused while the
//     group has a live worker instance or delivery rows, and force sweeps
//     every row the group owns.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/consumergroup"
	consumergroupcontroller "github.com/agentstax/vulkan/pkg/consumergroup/controller"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/topic"
	vulkan "github.com/agentstax/vulkan/pkg/vulkan"
	workercontroller "github.com/agentstax/vulkan/pkg/worker/controller"
	"github.com/jackc/pgx/v5/pgxpool"
)

type labMessage struct {
	N int `json:"n"`
}

func (labMessage) SchemaVersion() int { return 1 }

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
	ctx := context.Background()

	pool, err := vulkan.NewPostgresPool(ctx, "example_user", "example_password", "localhost", "example_db", nil)
	must(err)
	defer pool.Close()

	client, err := vulkan.NewClient(ctx, pool, &vulkan.ClientConfig{AllowDestroy: true})
	must(err)
	ds := client.Datastore()

	cd, err := consumergroupcontroller.NewConsumerGroupController(ds, nil)
	must(err)

	suffix := time.Now().UnixNano()
	topicA, err := client.RegisterTopic(ctx, fmt.Sprintf("consumergrouplab.a.%d", suffix), nil)
	must(err)
	topicB, err := client.RegisterTopic(ctx, fmt.Sprintf("consumergrouplab.b.%d", suffix), nil)
	must(err)

	step("RegisterGroup registers the group with its children in one txn")
	group := fmt.Sprintf("consumergrouplab.group.%d", suffix)
	registered, err := cd.RegisterGroup(ctx, topicA.Id, group, consumergroup.Beginning())
	must(err)
	g, err := cd.GetGroup(ctx, topicA.Id, group)
	must(err)
	if g == nil || g.Id != registered.Id || g.TopicId != topicA.Id || g.CreatedAt.IsZero() {
		die(fmt.Sprintf("GetGroup returned %+v, want id %d on topic %d with created_at set", g, registered.Id, topicA.Id))
	}
	assertChildren(ctx, ds, topicA.Id, registered.Id, 1, "at registration")
	fmt.Printf("  ✓ group %q (id %d) on topic %d, cursor created with it\n", group, registered.Id, topicA.Id)

	step("same name on a second topic is a DIFFERENT group")
	other, err := cd.RegisterGroup(ctx, topicB.Id, group, consumergroup.Beginning())
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
			_, err := cd.RegisterGroup(ctx, topicA.Id, race, consumergroup.Beginning())
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		must(err)
	}
	var raceRows int
	must(ds.Pool.QueryRow(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %s.consumer_group_config WHERE topic_id = $1 AND name = $2;`, ds.Schema), topicA.Id, race).Scan(&raceRows))
	if raceRows != 1 {
		die(fmt.Sprintf("race group has %d registry rows, want 1", raceRows))
	}
	fmt.Printf("  ✓ 10 concurrent registrations -> one registry row\n")

	step("Start: consumergroup.Head() places a new group's cursor at MAX(id); an existing group keeps its position")
	producing, err := client.RegisterProducer[labMessage](ctx, topicA.Name, nil)
	must(err)
	var seededHead int64
	for n := 1; n <= 3; n++ {
		produced, err := producing.Produce(ctx, &labMessage{N: n}, nil)
		must(err)
		seededHead = produced.Id
	}
	headGroup := fmt.Sprintf("consumergrouplab.head.%d", suffix)
	headInstance, err := client.RegisterConsumer[labMessage](ctx, headGroup, topicA.Name, &vulkan.ConsumerConfig{
		Start: vulkan.Head(),
	})
	must(err)
	assertCursor(ctx, ds, topicA.Id, headGroup, seededHead, "after Register at the head")
	fresh, err := producing.Produce(ctx, &labMessage{N: 4}, nil)
	must(err)
	consumeCtx, stop := context.WithCancel(ctx)
	time.AfterFunc(20*time.Second, stop)
	var seen []int
	consumeErr := headInstance.Consume(consumeCtx, func(ctx context.Context, message *labMessage) error {
		seen = append(seen, message.N)
		stop()
		return nil
	}, &vulkan.ConsumeOptions{ClaimPollRate: 200 * time.Millisecond})
	if consumeErr != nil && !errors.Is(consumeErr, context.Canceled) {
		must(consumeErr)
	}
	if len(seen) != 1 || seen[0] != 4 {
		die(fmt.Sprintf("group at the head saw %v, want only the post-register message 4 (id %d)", seen, fresh.Id))
	}
	before := readCursor(ctx, ds, topicA.Id, headGroup)
	_, err = client.RegisterConsumer[labMessage](ctx, headGroup, topicA.Name, nil)
	must(err)
	assertCursor(ctx, ds, topicA.Id, headGroup, before, "after a second Register at the beginning")
	fmt.Printf("  ✓ cursor created at %d, only message 4 delivered, a later Register left the row alone\n", seededHead)

	step("destroying a topic destroys ITS groups and no one else's")
	must(client.Topic(topicB.Name).Destroy(ctx, &vulkan.DestroyOptions{Force: true}))
	var bRows int
	must(ds.Pool.QueryRow(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %s.consumer_group_config WHERE id = $1;`, ds.Schema), other.Id).Scan(&bRows))
	if bRows != 0 {
		die("topicB's group survived its topic's Destroy")
	}
	// topicB's per-topic tables are dropped with it -- the cursor rows are
	// gone because their whole table is
	var cursorTable *string
	must(ds.Pool.QueryRow(ctx, `SELECT to_regclass($1)::text;`, fmt.Sprintf("%s.%s", ds.Schema, topic.ConsumerGroupCursorTable(topicB.Id))).Scan(&cursorTable))
	if cursorTable != nil {
		die("topicB's consumer_group_cursor table survived its topic's Destroy")
	}
	if g, err := cd.GetGroup(ctx, topicA.Id, group); err != nil || g == nil || g.Id != registered.Id {
		die(fmt.Sprintf("topic destroy touched the OTHER topic's group: %+v err=%v", g, err))
	}
	assertChildren(ctx, ds, topicA.Id, registered.Id, 1, "after topicB's destroy")
	fmt.Printf("  ✓ topicB's group + children cascaded away, topicA's same-named group untouched\n")

	step("deleting a group row cascades its cursor")
	if _, err := ds.Pool.Exec(ctx, fmt.Sprintf(`DELETE FROM %s.consumer_group_config WHERE id = $1;`, ds.Schema), registered.Id); err != nil {
		die(err.Error())
	}
	gone, err := cd.GetGroup(ctx, topicA.Id, group)
	must(err)
	if gone != nil {
		die(fmt.Sprintf("GetGroup still resolves the deleted group: %+v", gone))
	}
	assertChildren(ctx, ds, topicA.Id, registered.Id, 0, "after the group row's delete")
	fmt.Printf("  ✓ group %d deleted, cursor cascaded away\n", registered.Id)

	destroySection(ctx, pool, client, cd, topicA, suffix)

	// cleanup
	_, err = ds.Pool.Exec(ctx, fmt.Sprintf(`DELETE FROM %s.consumer_group_config WHERE topic_id = $1 AND name = $2;`, ds.Schema), topicA.Id, race)
	must(err)
	must(client.Topic(topicA.Name).Destroy(ctx, &vulkan.DestroyOptions{Force: true}))

	fmt.Printf("\n✅ consumer group registry lab PASSED\n")
	return nil
}

func destroySection(ctx context.Context, pool *pgxpool.Pool, client *vulkan.Client, cd *consumergroupcontroller.ConsumerGroupController, topicA *topic.TopicData, suffix int64) {
	step("DestroyGroup: gate + not-found, live/backlogged guards, force sweeps everything")

	doomedName := fmt.Sprintf("consumergrouplab.doomed.%d", suffix)
	doomed, err := cd.RegisterGroup(ctx, topicA.Id, doomedName, consumergroup.Beginning())
	must(err)
	_, err = cd.DeclareBindings(ctx, topicA.Id, doomed.Id, []string{"some.routing.key"}, time.Now())
	must(err)

	locked, err := vulkan.NewClient(ctx, pool, nil)
	must(err)

	ds := locked.Datastore()
	if err := locked.Topic(topicA.Name).Group(doomedName).Destroy(ctx, nil); !errors.Is(err, topic.ErrDestroyDisabled) {
		die(fmt.Sprintf("destroy without AllowDestroy: want ErrDestroyDisabled, got %v", err))
	}
	if err := client.Topic(topicA.Name).Group(doomedName+".missing").Destroy(ctx, nil); !errors.Is(err, consumergroup.ErrGroupNotFound) {
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
	if err := client.Topic(topicA.Name).Group(doomedName).Destroy(ctx, nil); !errors.Is(err, consumergroup.ErrGroupLive) {
		die(fmt.Sprintf("destroy with a live worker instance: want ErrGroupLive, got %v", err))
	}
	must(workers.ReleaseInstance(ctx, claimed.Id, claimed.Token))
	fmt.Printf("  ✓ live worker instance refuses the destroy\n")

	// delivery rows refuse it; force discards them along with the rows no FK
	// reaches (claim_lease, message_key_lease, delivery_log)
	_, err = ds.Pool.Exec(ctx, fmt.Sprintf(`INSERT INTO %s.%s (consumer_group_id, message_id, status, concurrency) VALUES ($1, 1, 'ready', 'parallel');`, ds.Schema, topic.ExceptionQueueTable(topicA.Id)), doomed.Id)
	must(err)
	_, err = ds.Pool.Exec(ctx, fmt.Sprintf(`INSERT INTO %s.%s (consumer_group_id, low, high, expires_at) VALUES ($1, 1, 10, now() + interval '1 minute');`, ds.Schema, topic.ClaimLeaseTable(topicA.Id)), doomed.Id)
	must(err)
	_, err = ds.Pool.Exec(ctx, fmt.Sprintf(`INSERT INTO %s.%s (consumer_group_id, message_id, attempt, status, error) VALUES ($1, 1, 1, 'failure', 'lab');`, ds.Schema, topic.DeliveryLogTable(topicA.Id)), doomed.Id)
	must(err)
	_, err = ds.Pool.Exec(ctx, fmt.Sprintf(`INSERT INTO %s.%s (consumer_group_id, message_key, lease_token, expires_at) VALUES ($1, 'labkey', gen_random_uuid(), now());`, ds.Schema, topic.MessageKeyLeaseTable(topicA.Id)), doomed.Id)
	must(err)
	if err := client.Topic(topicA.Name).Group(doomedName).Destroy(ctx, nil); !errors.Is(err, consumergroup.ErrGroupDeliveriesPending) {
		die(fmt.Sprintf("destroy with delivery rows: want ErrGroupDeliveriesPending, got %v", err))
	}
	must(client.Topic(topicA.Name).Group(doomedName).Destroy(ctx, &vulkan.DestroyOptions{Force: true}))

	for what, sql := range map[string]string{
		"group rows":        fmt.Sprintf(`SELECT COUNT(*) FROM %s.consumer_group_config WHERE id = $1;`, ds.Schema),
		"cursor rows":       fmt.Sprintf(`SELECT COUNT(*) FROM %s.%s WHERE consumer_group_id = $1;`, ds.Schema, topic.ConsumerGroupCursorTable(topicA.Id)),
		"binding rows":      fmt.Sprintf(`SELECT COUNT(*) FROM %s.%s WHERE consumer_group_id = $1;`, ds.Schema, topic.BindingConfigTable(topicA.Id)),
		"worker rows":       fmt.Sprintf(`SELECT COUNT(*) FROM %s.worker_config WHERE consumer_group_id = $1;`, ds.Schema),
		"instance rows":     fmt.Sprintf(`SELECT COUNT(*) FROM %s.worker_instance wi WHERE wi.worker_id IN (SELECT id FROM %s.worker_config WHERE consumer_group_id = $1);`, ds.Schema, ds.Schema),
		"lease rows":        fmt.Sprintf(`SELECT COUNT(*) FROM %s.%s WHERE consumer_group_id = $1;`, ds.Schema, topic.ClaimLeaseTable(topicA.Id)),
		"key lease rows":    fmt.Sprintf(`SELECT COUNT(*) FROM %s.%s WHERE consumer_group_id = $1;`, ds.Schema, topic.MessageKeyLeaseTable(topicA.Id)),
		"delivery rows":     fmt.Sprintf(`SELECT COUNT(*) FROM %s.%s WHERE consumer_group_id = $1;`, ds.Schema, topic.ExceptionQueueTable(topicA.Id)),
		"delivery log rows": fmt.Sprintf(`SELECT COUNT(*) FROM %s.%s WHERE consumer_group_id = $1;`, ds.Schema, topic.DeliveryLogTable(topicA.Id)),
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

// readCursor reads the group's committed cursor id by group name.
func readCursor(ctx context.Context, ds *iDatastore.PostgresDatastore, topicId int64, group string) int64 {
	var committed int64
	sql := fmt.Sprintf(`
		SELECT c.committed
		FROM %s.%s c JOIN %s.consumer_group_config g ON g.id = c.consumer_group_id
		WHERE g.topic_id = $1 AND g.name = $2;
	`, ds.Schema, topic.ConsumerGroupCursorTable(topicId), ds.Schema)
	must(ds.Pool.QueryRow(ctx, sql, topicId, group).Scan(&committed))
	return committed
}

func assertCursor(ctx context.Context, ds *iDatastore.PostgresDatastore, topicId int64, group string, want int64, when string) {
	committed := readCursor(ctx, ds, topicId, group)
	if committed != want {
		die(fmt.Sprintf("group %q committed = %d %s, want %d", group, committed, when, want))
	}
}

// assertChildren counts the group's cursor row -- it exists and dies
// together with the registry row (want 1 or 0).
func assertChildren(ctx context.Context, ds *iDatastore.PostgresDatastore, topicId int64, groupId int64, want int, when string) {
	var cursors int
	must(ds.Pool.QueryRow(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %s.%s WHERE consumer_group_id = $1;`, ds.Schema, topic.ConsumerGroupCursorTable(topicId)), groupId).Scan(&cursors))
	if cursors != want {
		die(fmt.Sprintf("group %d has %d cursors %s, want %d", groupId, cursors, when, want))
	}
}

func step(s string) { fmt.Printf("\n--- %s ---\n", s) }
func must(err error) {
	if err != nil {
		die(err.Error())
	}
}
func die(msg string) {
	panic(labFailure{message: msg})
}
