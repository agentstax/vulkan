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

	"github.com/agentstax/vulkan/pkg/admin"
	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/consumer"
	"github.com/agentstax/vulkan/pkg/consumergroup"
	consumergroupcontroller "github.com/agentstax/vulkan/pkg/consumergroup/controller"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/topic"
	workercontroller "github.com/agentstax/vulkan/pkg/worker/controller"
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

	ds, err := iDatastore.NewPostgresDatastore(ctx, "example_user", "localhost", "example_db", &iDatastore.PostgresConnectionConfig{Pass: "example_password"})
	must(err)
	defer ds.Close()

	mAdmin, err := admin.NewMessageAdmin(ds, &admin.MessageAdminConfig{AllowDestroy: true})
	must(err)
	must(mAdmin.RegisterSystem(ctx, nil))

	cd, err := consumergroupcontroller.NewConsumerGroupController(ds, nil)
	must(err)

	suffix := time.Now().UnixNano()
	topicA, err := mAdmin.RegisterTopic(ctx, fmt.Sprintf("consumergrouplab.a.%d", suffix), nil)
	must(err)
	topicB, err := mAdmin.RegisterTopic(ctx, fmt.Sprintf("consumergrouplab.b.%d", suffix), nil)
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
	must(ds.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM consumer_group_config WHERE topic_id = $1 AND name = $2;`, topicA.Id, race).Scan(&raceRows))
	if raceRows != 1 {
		die(fmt.Sprintf("race group has %d registry rows, want 1", raceRows))
	}
	fmt.Printf("  ✓ 10 concurrent registrations -> one registry row\n")

	step("Start: consumergroup.Head() places a new group's cursor at MAX(id); an existing group keeps its position")
	labProducer, err := producer.NewProducer(ds, nil)
	must(err)
	producing, err := labProducer.Register[labMessage](ctx, topicA.Name)
	must(err)
	var seededHead int64
	for n := 1; n <= 3; n++ {
		produced, err := producing.Produce(ctx, &labMessage{N: n}, producer.ProduceOptions{})
		must(err)
		seededHead = produced.Id
	}
	headConsumer, err := consumer.NewConsumer(ds, &consumer.ConsumerConfig{
		Start:         consumergroup.Head(),
		ClaimPollRate: 200 * time.Millisecond,
	})
	must(err)
	headGroup := fmt.Sprintf("consumergrouplab.head.%d", suffix)
	headInstance, err := headConsumer.Register[labMessage](ctx, headGroup, topicA.Name, nil)
	must(err)
	assertCursor(ctx, ds, topicA.Id, headGroup, seededHead, "after Register at the head")
	fresh, err := producing.Produce(ctx, &labMessage{N: 4}, producer.ProduceOptions{})
	must(err)
	consumeCtx, stop := context.WithCancel(ctx)
	time.AfterFunc(20*time.Second, stop)
	var seen []int
	consumeErr := headInstance.Consume(consumeCtx, func(ctx context.Context, message *labMessage) error {
		seen = append(seen, message.N)
		stop()
		return nil
	})
	if consumeErr != nil && !errors.Is(consumeErr, context.Canceled) {
		must(consumeErr)
	}
	if len(seen) != 1 || seen[0] != 4 {
		die(fmt.Sprintf("group at the head saw %v, want only the post-register message 4 (id %d)", seen, fresh.Id))
	}
	before := readCursor(ctx, ds, topicA.Id, headGroup)
	beginningConsumer, err := consumer.NewConsumer(ds, nil)
	must(err)
	_, err = beginningConsumer.Register[labMessage](ctx, headGroup, topicA.Name, nil)
	must(err)
	assertCursor(ctx, ds, topicA.Id, headGroup, before, "after a second Register at the beginning")
	fmt.Printf("  ✓ cursor created at %d, only message 4 delivered, a later Register left the row alone\n", seededHead)

	step("destroying a topic destroys ITS groups and no one else's")
	must(mAdmin.DestroyTopic(ctx, topicB.Name, admin.DestroyOptions{Force: true}))
	var bRows int
	must(ds.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM consumer_group_config WHERE id = $1;`, other.Id).Scan(&bRows))
	if bRows != 0 {
		die("topicB's group survived its topic's Destroy")
	}
	// topicB's per-topic tables are dropped with it -- the cursor rows are
	// gone because their whole table is
	var cursorTable *string
	must(ds.Pool.QueryRow(ctx, `SELECT to_regclass($1)::text;`, fmt.Sprintf("consumer_group_cursor_%d", topicB.Id)).Scan(&cursorTable))
	if cursorTable != nil {
		die("topicB's consumer_group_cursor table survived its topic's Destroy")
	}
	if g, err := cd.GetGroup(ctx, topicA.Id, group); err != nil || g == nil || g.Id != registered.Id {
		die(fmt.Sprintf("topic destroy touched the OTHER topic's group: %+v err=%v", g, err))
	}
	assertChildren(ctx, ds, topicA.Id, registered.Id, 1, "after topicB's destroy")
	fmt.Printf("  ✓ topicB's group + children cascaded away, topicA's same-named group untouched\n")

	step("deleting a group row cascades its cursor")
	if _, err := ds.Pool.Exec(ctx, `DELETE FROM consumer_group_config WHERE id = $1;`, registered.Id); err != nil {
		die(err.Error())
	}
	gone, err := cd.GetGroup(ctx, topicA.Id, group)
	must(err)
	if gone != nil {
		die(fmt.Sprintf("GetGroup still resolves the deleted group: %+v", gone))
	}
	assertChildren(ctx, ds, topicA.Id, registered.Id, 0, "after the group row's delete")
	fmt.Printf("  ✓ group %d deleted, cursor cascaded away\n", registered.Id)

	destroySection(ctx, ds, mAdmin, cd, topicA, suffix)

	// cleanup
	_, err = ds.Pool.Exec(ctx, `DELETE FROM consumer_group_config WHERE topic_id = $1 AND name = $2;`, topicA.Id, race)
	must(err)
	must(mAdmin.DestroyTopic(ctx, topicA.Name, admin.DestroyOptions{Force: true}))

	fmt.Printf("\n✅ consumer group registry lab PASSED\n")
	return nil
}

func destroySection(ctx context.Context, ds *iDatastore.PostgresDatastore, mAdmin *admin.MessageAdmin, cd *consumergroupcontroller.ConsumerGroupController, topicA *topic.Topic, suffix int64) {
	step("DestroyGroup: gate + not-found, live/backlogged guards, force sweeps everything")

	doomedName := fmt.Sprintf("consumergrouplab.doomed.%d", suffix)
	doomed, err := cd.RegisterGroup(ctx, topicA.Id, doomedName, consumergroup.Beginning())
	must(err)
	_, err = cd.DeclareBindings(ctx, topicA.Id, doomed.Id, []string{"some.routing.key"}, time.Now())
	must(err)

	locked, err := admin.NewMessageAdmin(ds, nil)
	must(err)
	if err := locked.DestroyGroup(ctx, topicA.Name, doomedName, admin.DestroyOptions{}); !errors.Is(err, topic.ErrDestroyDisabled) {
		die(fmt.Sprintf("destroy without AllowDestroy: want ErrDestroyDisabled, got %v", err))
	}
	if err := mAdmin.DestroyGroup(ctx, topicA.Name, doomedName+".missing", admin.DestroyOptions{}); !errors.Is(err, consumergroup.ErrGroupNotFound) {
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
	if err := mAdmin.DestroyGroup(ctx, topicA.Name, doomedName, admin.DestroyOptions{}); !errors.Is(err, consumergroup.ErrGroupLive) {
		die(fmt.Sprintf("destroy with a live worker instance: want ErrGroupLive, got %v", err))
	}
	must(workers.ReleaseInstance(ctx, claimed.Id, claimed.Token))
	fmt.Printf("  ✓ live worker instance refuses the destroy\n")

	// delivery rows refuse it; force discards them along with the rows no FK
	// reaches (claim_lease, message_key_lease, delivery_log)
	_, err = ds.Pool.Exec(ctx, fmt.Sprintf(`INSERT INTO exception_queue_%d (consumer_group_id, message_id, status, concurrency) VALUES ($1, 1, 'ready', 'parallel');`, topicA.Id), doomed.Id)
	must(err)
	_, err = ds.Pool.Exec(ctx, fmt.Sprintf(`INSERT INTO claim_lease_%d (consumer_group_id, low, high, expires_at) VALUES ($1, 1, 10, now() + interval '1 minute');`, topicA.Id), doomed.Id)
	must(err)
	_, err = ds.Pool.Exec(ctx, fmt.Sprintf(`INSERT INTO delivery_log_%d (consumer_group_id, message_id, attempt, status, error) VALUES ($1, 1, 1, 'failure', 'lab');`, topicA.Id), doomed.Id)
	must(err)
	_, err = ds.Pool.Exec(ctx, fmt.Sprintf(`INSERT INTO message_key_lease_%d (consumer_group_id, message_key, lease_token, expires_at) VALUES ($1, 'labkey', gen_random_uuid(), now());`, topicA.Id), doomed.Id)
	must(err)
	if err := mAdmin.DestroyGroup(ctx, topicA.Name, doomedName, admin.DestroyOptions{}); !errors.Is(err, consumergroup.ErrGroupDeliveriesPending) {
		die(fmt.Sprintf("destroy with delivery rows: want ErrGroupDeliveriesPending, got %v", err))
	}
	must(mAdmin.DestroyGroup(ctx, topicA.Name, doomedName, admin.DestroyOptions{Force: true}))

	for what, sql := range map[string]string{
		"group rows":        `SELECT COUNT(*) FROM consumer_group_config WHERE id = $1;`,
		"cursor rows":       fmt.Sprintf(`SELECT COUNT(*) FROM consumer_group_cursor_%d WHERE consumer_group_id = $1;`, topicA.Id),
		"binding rows":      fmt.Sprintf(`SELECT COUNT(*) FROM binding_config_%d WHERE consumer_group_id = $1;`, topicA.Id),
		"worker rows":       `SELECT COUNT(*) FROM worker_config WHERE consumer_group_id = $1;`,
		"instance rows":     `SELECT COUNT(*) FROM worker_instance wi WHERE wi.worker_id IN (SELECT id FROM worker_config WHERE consumer_group_id = $1);`,
		"lease rows":        fmt.Sprintf(`SELECT COUNT(*) FROM claim_lease_%d WHERE consumer_group_id = $1;`, topicA.Id),
		"key lease rows":    fmt.Sprintf(`SELECT COUNT(*) FROM message_key_lease_%d WHERE consumer_group_id = $1;`, topicA.Id),
		"delivery rows":     fmt.Sprintf(`SELECT COUNT(*) FROM exception_queue_%d WHERE consumer_group_id = $1;`, topicA.Id),
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

// readCursor reads the group's committed cursor id by group name.
func readCursor(ctx context.Context, ds *iDatastore.PostgresDatastore, topicId int64, group string) int64 {
	var committed int64
	sql := fmt.Sprintf(`
		SELECT c.committed
		FROM consumer_group_cursor_%d c JOIN consumer_group_config g ON g.id = c.consumer_group_id
		WHERE g.topic_id = $1 AND g.name = $2;
	`, topicId)
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
	must(ds.Pool.QueryRow(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM consumer_group_cursor_%d WHERE consumer_group_id = $1;`, topicId), groupId).Scan(&cursors))
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
