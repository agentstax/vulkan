// Command schemalab proves a schema is one Vulkan installation: two clients
// on two schemas in one database each register a topic under the same name
// and see only their own.
//
// Sections:
//  1. isolation -- both schemas hold their own full table set, the same topic
//     name registers in each with its own id, and each client lists only its
//     own topics
//  2. independence -- a message produced on one schema is invisible to the
//     other, destroying a topic on one leaves the other's standing, and a
//     caller's own CREATE inside InTransaction lands in the caller's schema
//     rather than vulkan's, because the pool sets no search_path [0632]
//  3. absence -- a client pointed at a schema nobody registered reads an
//     absence rather than the neighbouring schema's rows: Get is (nil, nil),
//     every other verb raises ErrTopicNotFound. Holds with a whole
//     installation standing in public, the schema every search_path ends
//     with -- vulkan's SQL names its own schema, so there is nothing to
//     fall through to
//  4. locks -- a register held up on one schema does not hold up the same
//     register on the other, and every key carries vulkan's namespace
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/common/logging"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/topic"
	vulkan "github.com/agentstax/vulkan/pkg/vulkan"
)

// labMessage is the payload both schemas produce.
type labMessage struct {
	Value string
}

func (labMessage) SchemaVersion() int { return 1 }

func main() {
	if err := run(); err != nil {
		fmt.Printf("\n❌ LAB FAILED: %s\n", err.Error())
		os.Exit(1)
	}
}

// labFailure is what die panics with; run recovers it into its error so the
// deferred cleanup runs on a failed assertion.
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

	// two schemas, not two databases -- the point is that one database holds
	// both installations and neither can see the other
	runId := time.Now().UnixNano()
	leftSchema := fmt.Sprintf("schemalab_left_%d", runId)
	rightSchema := fmt.Sprintf("schemalab_right_%d", runId)

	// ONE base config, copied per client: NewClient resolves the pointer it is
	// handed, so clients sharing one would all end on the last schema written
	// into it, and pipeline Args concatenate on merge -- a shared config would
	// name every installation on every line any of them logs
	shared := &vulkan.ClientConfig{AllowDestroy: true}
	left, leftDs := openClient(ctx, leftSchema, shared)
	defer leftDs.Pool.Close()
	right, rightDs := openClient(ctx, rightSchema, shared)
	defer rightDs.Pool.Close()
	defer dropSchemas(ctx, leftDs, leftSchema, rightSchema)

	must(left.RegisterSystem(ctx, nil))
	must(right.RegisterSystem(ctx, nil))

	fmt.Println("=== 1. isolation ===")

	leftTables := tableCount(ctx, leftDs, leftSchema)
	rightTables := tableCount(ctx, rightDs, rightSchema)
	if leftTables == 0 || leftTables != rightTables {
		die(fmt.Sprintf("each schema should hold its own full table set, got left %d right %d", leftTables, rightTables))
	}
	fmt.Printf("   ✅ both schemas hold their own %d tables\n", leftTables)

	// the same name in both -- a name is unique per installation, not per database
	const sharedName = "schemalab.orders"
	leftTopic, err := left.RegisterTopic(ctx, sharedName, nil)
	must(err)
	rightTopic, err := right.RegisterTopic(ctx, sharedName, nil)
	must(err)
	fmt.Printf("   ✅ %q registered in both, each numbered by its own schema's sequence: left id %d, right id %d\n", sharedName, leftTopic.Id, rightTopic.Id)

	leftTopics, err := left.ListTopics(ctx)
	must(err)
	rightTopics, err := right.ListTopics(ctx)
	must(err)
	if countNamed(leftTopics, sharedName) != 1 || countNamed(rightTopics, sharedName) != 1 {
		die("each client should list its own topic exactly once")
	}
	if len(leftTopics) != len(rightTopics) {
		die(fmt.Sprintf("neither client should see the other's topics, got left %d right %d", len(leftTopics), len(rightTopics)))
	}
	fmt.Printf("   ✅ each client lists %d topics -- its own, not the other's\n", len(leftTopics))

	leftBound, rightBound := boundSchemas(left.Logger), boundSchemas(right.Logger)
	if len(leftBound) != 1 || leftBound[0] != leftSchema || len(rightBound) != 1 || rightBound[0] != rightSchema {
		die(fmt.Sprintf("each client should bind only its own schema, got left %v right %v", leftBound, rightBound))
	}
	fmt.Printf("   ✅ every line each client logs names one schema -- %q and %q\n", leftBound[0], rightBound[0])

	fmt.Println("\n=== 2. independence ===")

	leftProducer, err := left.RegisterProducer[labMessage](ctx, sharedName, nil)
	must(err)
	_, err = leftProducer.Produce(ctx, &labMessage{Value: "left only"}, nil)
	must(err)

	leftCount := messageCount(ctx, leftDs, leftSchema, leftTopic.Id)
	rightCount := messageCount(ctx, rightDs, rightSchema, rightTopic.Id)
	if leftCount != 1 || rightCount != 0 {
		die(fmt.Sprintf("a produce on one schema must not reach the other, got left %d right %d", leftCount, rightCount))
	}
	fmt.Printf("   ✅ one produce on left: left holds %d, right holds %d\n", leftCount, rightCount)

	// a caller's own statement inside InTransaction runs on vulkan's pool, and
	// the pool sets no search_path [0632] -- so an unqualified CREATE lands
	// where the caller's connection puts it, not inside vulkan's schema
	const callerTable = "schemalab_caller_orders"
	must(left.InTransaction(ctx, func(ctx context.Context, tx vulkan.Tx) error {
		_, err := tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS `+callerTable+` (id BIGINT);`)
		return err
	}))
	var landedIn string
	must(leftDs.Pool.QueryRow(ctx,
		`SELECT schemaname FROM pg_tables WHERE tablename = $1;`, callerTable).Scan(&landedIn))
	defer func() {
		_, err := leftDs.Pool.Exec(ctx, `DROP TABLE IF EXISTS `+landedIn+`.`+callerTable+`;`)
		must(err)
	}()
	if landedIn == leftSchema {
		die("a caller's own CREATE inside InTransaction must not land in vulkan's schema, got " + landedIn)
	}
	fmt.Printf("   ✅ a caller's CREATE inside InTransaction lands in %q, not vulkan's %q\n", landedIn, leftSchema)

	must(left.Topic(sharedName).Destroy(ctx, &vulkan.DestroyOptions{Force: true}))
	if _, err := right.Topic(sharedName).Get(ctx); err != nil {
		die("destroying the left topic must leave the right one readable: " + err.Error())
	}
	fmt.Println("   ✅ destroying left's topic leaves right's standing")

	fmt.Println("\n=== 3. absence ===")

	emptySchema := fmt.Sprintf("schemalab_empty_%d", runId)
	empty, emptyDs := openClient(ctx, emptySchema, shared)
	defer emptyDs.Pool.Close()

	// the schema does not exist, and every vulkan statement names it [0631],
	// so the catalog read raises undefined_table -- which the catalog reads
	// map to absence, never to another installation's rows
	found, err := empty.Topic(sharedName).Get(ctx)
	must(err)
	if found != nil {
		die("an unregistered schema must read no topic, got " + found.Name)
	}
	fmt.Println("   ✅ Get on an unregistered schema is the (nil, nil) absence")

	if _, err := empty.Topic(sharedName).Health(ctx); !errors.Is(err, topic.ErrTopicNotFound) {
		die(fmt.Sprintf("every verb but Get should raise absence, got %v", err))
	}
	fmt.Println("   ✅ every other verb raises ErrTopicNotFound rather than reading a neighbour")

	// public is the one schema every search_path ends with, so an installation
	// there is what an unqualified statement falls through to. What keeps the
	// reads above absences is that vulkan's SQL names its own schema -- not
	// that public happens to be empty.
	publicClient, publicDs := openClient(ctx, "public", shared)
	defer publicDs.Pool.Close()
	must(publicClient.RegisterSystem(ctx, nil))
	defer func() { must(publicClient.System().Destroy(ctx, &vulkan.DestroyOptions{Force: true})) }()
	_, err = publicClient.RegisterTopic(ctx, sharedName, nil)
	must(err)

	found, err = empty.Topic(sharedName).Get(ctx)
	must(err)
	if found != nil {
		die("a full installation in public must not become the empty schema's answer, got " + found.Name)
	}
	fmt.Println("   ✅ with a whole installation standing in public, Get is still the absence")

	if _, err := empty.Topic(sharedName).Health(ctx); !errors.Is(err, topic.ErrTopicNotFound) {
		die(fmt.Sprintf("every verb but Get should still raise absence, got %v", err))
	}
	fmt.Println("   ✅ ...and every other verb still raises ErrTopicNotFound")

	fmt.Println("\n=== 4. locks ===")

	// hold the key vulkan derives for a register on the left schema -- the
	// register blocking on it is what proves the datastore takes the same one
	const lockedName = "schemalab.locked"
	lockKey, err := common.NewAdvisoryLockKey("topic", leftSchema, lockedName)
	must(err)

	holder, err := leftDs.Pool.Acquire(ctx)
	must(err)
	defer holder.Release()
	_, err = holder.Exec(ctx, `SELECT pg_advisory_lock($1);`, lockKey.Value())
	must(err)

	if lockKey.ClassId() != common.AdvisoryLockNamespace {
		die(fmt.Sprintf("every key should carry the namespace, got classid %d", lockKey.ClassId()))
	}

	// the same predicate migrate.isLocked reads: finding the lock by the two
	// halves proves ClassId and ObjId split it the way postgres filed it
	var held int
	must(leftDs.Pool.QueryRow(ctx, `
		SELECT count(*) FROM pg_locks
		WHERE locktype = 'advisory'
			AND classid = $1
			AND objid = $2
			AND objsubid = 1
			AND granted;
	`, lockKey.ClassId(), lockKey.ObjId()).Scan(&held))
	if held != 1 {
		die(fmt.Sprintf("pg_locks should hold the key under classid %d objid %d, found %d rows", lockKey.ClassId(), lockKey.ObjId(), held))
	}
	fmt.Printf("   ✅ the held lock reads back under classid %d -- vulkan's namespace -- and objid %d\n", lockKey.ClassId(), lockKey.ObjId())

	blocked, cancelBlocked := context.WithTimeout(ctx, 2*time.Second)
	defer cancelBlocked()
	if _, err := left.RegisterTopic(blocked, lockedName, nil); err == nil {
		die("registering under the held key should have waited, it returned")
	}
	fmt.Println("   ✅ the same schema's register waits on the held key")

	free, cancelFree := context.WithTimeout(ctx, 2*time.Second)
	defer cancelFree()
	if _, err := right.RegisterTopic(free, lockedName, nil); err != nil {
		die("the other schema's register must not wait on it: " + err.Error())
	}
	fmt.Println("   ✅ the other schema's register takes a different key and completes")

	_, err = holder.Exec(ctx, `SELECT pg_advisory_unlock($1);`, lockKey.Value())
	must(err)

	fmt.Println("\n✅ SCHEMA LAB PASSED")
	fmt.Println("   one schema is one installation: the same topic name registers in each,")
	fmt.Println("   messages and lifecycles are separate, no read crosses the boundary, and")
	fmt.Println("   neither installation waits on the other's locks.")
	return nil
}

// ***************
// *** HELPERS ***
// ***************

func openClient(ctx context.Context, schema string, cfg *vulkan.ClientConfig) (*vulkan.Client, *iDatastore.PostgresDatastore) {
	pool, err := iDatastore.NewPostgresPool(ctx, "example_user", "example_password", "localhost", "example_db", nil)
	must(err)

	clientConfig := *cfg
	clientConfig.Schema = schema

	client, err := vulkan.NewClient(ctx, pool, &clientConfig)
	must(err)
	return client, client.Datastore()
}

// boundSchemas lists the schema values a client's logger binds onto every
// line it writes.
func boundSchemas(logger logging.Logger) []string {
	pipeline, ok := logger.(*logging.PipelineLogger)
	if !ok {
		die("a client's logger should be a pipeline")
	}

	found := []string{}
	args := pipeline.Config.Args
	for i := 0; i+1 < len(args); i += 2 {
		if key, keyOk := args[i].(string); keyOk && key == "schema" {
			found = append(found, fmt.Sprint(args[i+1]))
		}
	}
	return found
}

func tableCount(ctx context.Context, ds *iDatastore.PostgresDatastore, schema string) int {
	var count int
	err := ds.Pool.QueryRow(ctx,
		`SELECT count(*) FROM information_schema.tables WHERE table_schema = $1;`, schema).Scan(&count)
	must(err)
	return count
}

func messageCount(ctx context.Context, ds *iDatastore.PostgresDatastore, schema string, topicId int64) int {
	var count int
	err := ds.Pool.QueryRow(ctx,
		fmt.Sprintf(`SELECT count(*) FROM %s.%s;`, schema, topic.MessageLogTable(topicId))).Scan(&count)
	must(err)
	return count
}

func countNamed(topics []*vulkan.TopicData, name string) int {
	found := 0
	for _, data := range topics {
		if data.Name == name {
			found++
		}
	}
	return found
}

func dropSchemas(ctx context.Context, ds *iDatastore.PostgresDatastore, schemas ...string) {
	for _, schema := range schemas {
		if _, err := ds.Pool.Exec(ctx, fmt.Sprintf(`DROP SCHEMA IF EXISTS %s CASCADE;`, schema)); err != nil {
			fmt.Printf("   cleanup: %s\n", err.Error())
		}
	}
}

func must(err error) {
	if err != nil {
		die(err.Error())
	}
}

func die(msg string) {
	panic(labFailure{message: msg})
}
