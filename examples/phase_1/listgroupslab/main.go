package main

// ListGroups lab: proves the consumer-group list read and the Topic / Group
// handles against a live database -- a topic's groups list in name order,
// Group.Get returns the row, absence is (nil, nil) on Get and
// ErrTopicNotFound on every other verb, and Topic.Destroy drops the family.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	vulkan "github.com/agentstax/vulkan/pkg/vulkan"
	"github.com/agentstax/vulkan/pkg/consumergroup"
	consumergroupcontroller "github.com/agentstax/vulkan/pkg/consumergroup/controller"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/topic"
)

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
	run := time.Now().UnixNano()

	ds, err := iDatastore.NewPostgresDatastore(ctx, "example_user", "localhost", "example_db", &iDatastore.PostgresConnectionConfig{Pass: "example_password"})
	must(err)
	defer ds.Close()

	client, err := vulkan.NewClient(ds, &vulkan.ClientConfig{AllowDestroy: true})
	must(err)

	name := fmt.Sprintf("listgroupslab.orders.%d", run)
	registered, err := client.RegisterTopic(ctx, name, nil)
	must(err)

	groupController, err := consumergroupcontroller.NewConsumerGroupController(ds, nil)
	must(err)

	step("seed two groups on the topic")
	for _, groupName := range []string{"beta", "alpha"} {
		_, err := groupController.RegisterGroup(ctx, registered.Id, groupName, consumergroup.Beginning())
		must(err)
	}

	step("Topic.ListGroups returns both, ordered by name")
	orders := client.Topic(name)
	groups, err := orders.ListGroups(ctx)
	must(err)
	if len(groups) != 2 {
		die(fmt.Sprintf("expected 2 groups, got %d", len(groups)))
	}
	assertString("first group", groups[0].Name, "alpha")
	assertString("second group", groups[1].Name, "beta")
	assertInt64("group topic id", groups[0].TopicId, registered.Id)

	step("Group.Get returns the row")
	alpha, err := orders.Group("alpha").Get(ctx)
	must(err)
	if alpha == nil {
		die("expected the alpha row, got nil")
	}
	assertInt64("alpha id", alpha.Id, groups[0].Id)

	step("absence is (nil, nil) on Get only")
	ghost, err := orders.Group("ghost").Get(ctx)
	must(err)
	if ghost != nil {
		die(fmt.Sprintf("expected (nil, nil) for an unregistered group, got %+v", ghost))
	}
	ghostTopic := client.Topic(fmt.Sprintf("listgroupslab.ghost.%d", run))
	row, err := ghostTopic.Get(ctx)
	must(err)
	if row != nil {
		die(fmt.Sprintf("expected (nil, nil) for an unregistered topic, got %+v", row))
	}
	_, err = ghostTopic.ListGroups(ctx)
	if !errors.Is(err, topic.ErrTopicNotFound) {
		die(fmt.Sprintf("ListGroups on an unregistered topic: expected ErrTopicNotFound, got %v", err))
	}
	fmt.Printf("  ✓ ListGroups on an unregistered topic -> %v\n", err)

	step("cleanup: Topic.Destroy drops the family")
	must(orders.Destroy(ctx, &vulkan.DestroyOptions{Force: true}))
	gone, err := orders.Get(ctx)
	must(err)
	if gone != nil {
		die("expected the topic row gone after Destroy")
	}

	fmt.Println("\n✅ LIST GROUPS LAB PASSED")
	return nil
}

func assertString(label string, got string, want string) {
	if got != want {
		die(fmt.Sprintf("%s: got %q, want %q", label, got, want))
	}
	fmt.Printf("  ✓ %s (%q)\n", label, got)
}

func assertInt64(label string, got int64, want int64) {
	if got != want {
		die(fmt.Sprintf("%s: got %d, want %d", label, got, want))
	}
	fmt.Printf("  ✓ %s (%d)\n", label, got)
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
