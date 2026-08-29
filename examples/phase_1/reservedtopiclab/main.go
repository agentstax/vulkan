package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/agentstax/vulkan/pkg/admin"
	"github.com/agentstax/vulkan/pkg/common"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/metrics"
	metricscontroller "github.com/agentstax/vulkan/pkg/metrics/controller"
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

	mAdmin, err := admin.NewMessageAdmin(ds, &admin.MessageAdminConfig{AllowDestroy: true})
	must(err)

	step("RegisterSystem creates __system.metrics idempotently")
	must(mAdmin.RegisterSystem(ctx, nil))
	metricsTopic, err := mAdmin.GetTopic(ctx, metrics.TopicName, topic.SchemaVersion(1))
	must(err)
	if metricsTopic == nil {
		die("expected __system.metrics to exist after RegisterSystem")
	}
	fmt.Printf("  ✓ __system.metrics exists, id=%d, retention=%v, partition_size=%d, delivery_log=%s\n",
		metricsTopic.Id, metricsTopic.RetentionTTL, metricsTopic.PartitionSize, metricsTopic.DeliveryLogMode)

	step("RegisterTopic rejects a user name under the reserved prefix")
	_, err = mAdmin.RegisterTopic(ctx, common.SystemTopicPrefix+"evil", topic.SchemaVersion(1), nil)
	assertReserved("RegisterTopic(__system.evil)", err)

	step("RenameTopic refused both directions")
	_, err = mAdmin.RenameTopic(ctx, metrics.TopicName, fmt.Sprintf("reservedtopiclab.stolen.%d", run))
	assertReserved("RenameTopic(__system.metrics -> user name)", err)

	userTopic, err := mAdmin.RegisterTopic(ctx, fmt.Sprintf("reservedtopiclab.user.%d", run), topic.SchemaVersion(1), nil)
	must(err)
	_, err = mAdmin.RenameTopic(ctx, userTopic.Name, common.SystemTopicPrefix+"evil")
	assertReserved("RenameTopic(user name -> __system.evil)", err)
	must(mAdmin.DestroyTopic(ctx, userTopic.Name, topic.SchemaVersion(1), admin.DestroyOptions{Force: true}))

	step("DestroyTopic refused on the system topic")
	err = mAdmin.DestroyTopic(ctx, metrics.TopicName, topic.SchemaVersion(1), admin.DestroyOptions{Force: true})
	assertReserved("DestroyTopic(__system.metrics)", err)

	step("re-running RegisterSystem keeps the same row and re-declares its config")
	must(mAdmin.RegisterSystem(ctx, nil))
	afterRerun, err := mAdmin.GetTopic(ctx, metrics.TopicName, topic.SchemaVersion(1))
	must(err)
	assertInt64("topic id unchanged across re-run", afterRerun.Id, metricsTopic.Id)
	assertDuration("declared retention across re-run", afterRerun.RetentionTTL, metricscontroller.TopicConfig().RetentionTTL)

	fmt.Println("\n✅ RESERVED TOPIC LAB PASSED")
	return nil
}

func assertReserved(label string, err error) {
	if !errors.Is(err, topic.ErrReservedTopicName) {
		die(fmt.Sprintf("%s: expected ErrReservedTopicName, got %v", label, err))
	}
	fmt.Printf("  ✓ %s -> %v\n", label, err)
}

func assertDuration(label string, got, want time.Duration) {
	if got != want {
		die(fmt.Sprintf("%s: got %v, want %v", label, got, want))
	}
	fmt.Printf("  ✓ %s (%v)\n", label, got)
}

func assertInt64(label string, got, want int64) {
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
