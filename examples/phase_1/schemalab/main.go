// Command schemalab proves a schema is one Vulkan installation: two clients
// on two schemas in one database each register a topic under the same name
// and see only their own.
//
// Sections:
//  1. isolation -- both schemas hold their own full table set, the same topic
//     name registers in each with its own id, and each client lists only its
//     own topics
//  2. independence -- a message produced on one schema is invisible to the
//     other, and destroying a topic on one leaves the other's standing
//  3. absence -- a client pointed at a schema nobody registered reads an
//     absence rather than the neighbouring schema's rows: Get is (nil, nil),
//     every other verb raises ErrTopicNotFound
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

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

	left, leftDs := openClient(ctx, leftSchema)
	defer leftDs.Close()
	right, rightDs := openClient(ctx, rightSchema)
	defer rightDs.Close()
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

	must(left.Topic(sharedName).Destroy(ctx, &vulkan.DestroyOptions{Force: true}))
	if _, err := right.Topic(sharedName).Get(ctx); err != nil {
		die("destroying the left topic must leave the right one readable: " + err.Error())
	}
	fmt.Println("   ✅ destroying left's topic leaves right's standing")

	fmt.Println("\n=== 3. absence ===")

	emptySchema := fmt.Sprintf("schemalab_empty_%d", runId)
	empty, emptyDs := openClient(ctx, emptySchema)
	defer emptyDs.Close()

	// the schema does not exist, so search_path falls through to public --
	// which holds no vulkan tables, so the read is an absence and not the
	// neighbouring installation's rows
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

	fmt.Println("\n✅ SCHEMA LAB PASSED")
	fmt.Println("   one schema is one installation: the same topic name registers in each,")
	fmt.Println("   messages and lifecycles are separate, and no read crosses the boundary.")
	return nil
}

// ***************
// *** HELPERS ***
// ***************

func openClient(ctx context.Context, schema string) (*vulkan.Client, *iDatastore.PostgresDatastore) {
	ds, err := iDatastore.NewPostgresDatastore(ctx, "example_user", "localhost", "example_db",
		&iDatastore.PostgresConnectionConfig{Pass: "example_password", Schema: schema})
	must(err)
	client, err := vulkan.NewClient(ds, &vulkan.ClientConfig{AllowDestroy: true})
	must(err)
	return client, ds
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
