package main

// register idempotency lab: re-registering a topic resolves to the same row,
// and the newest declaration's mutable config replaces what is stored -- guarding
// registerTopic's found path (replaceConfig).
//
// Confirms:
//  1. first Register creates the topic and appends its first topic_config_log row.
//  2. re-registering the SAME config resolves to the same topic, no error and
//     no write -- topic_config_log gains nothing.
//  3. re-registering DIFFERENT mutable config keeps the id, replaces the
//     values, and appends the new snapshot to topic_config_log.
//  4. re-registering a different PartitionSize returns ErrTopicConfigMismatch:
//     message_log's partition boundaries are derived from it.

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

	client, err := vulkan.NewClient(ds, &vulkan.ClientConfig{AllowDestroy: true})
	must(err)

	name := fmt.Sprintf("registeridempotency.lab.%d", time.Now().UnixNano())

	step("first register creates the topic")
	created, err := client.RegisterTopic(ctx, name, &vulkan.TopicConfig{RetentionTTL: 720 * time.Hour})
	must(err)
	defer func() {
		must(client.Topic(name).Destroy(ctx, vulkan.DestroyOptions{Force: true}))
	}()
	if count := topicLogCount(ctx, ds, created.Id); count != 1 {
		die(fmt.Sprintf("topic_config_log rows after create = %d, want 1", count))
	}
	fmt.Printf("  ✓ created id=%d, first topic_config_log row appended\n", created.Id)

	step("re-register SAME config is idempotent, not a mismatch")
	// Fresh Config with the identical caller-set field -- RegisterTopic mutates
	// what it's given via WithDefaults, so don't reuse the first one.
	again, err := client.RegisterTopic(ctx, name, &vulkan.TopicConfig{RetentionTTL: 720 * time.Hour})
	if err != nil {
		die(fmt.Sprintf("re-register with identical config must succeed, got: %v", err))
	}
	if again.Id != created.Id {
		die(fmt.Sprintf("re-register resolved a different id: got %d, want %d", again.Id, created.Id))
	}
	if count := topicLogCount(ctx, ds, created.Id); count != 1 {
		die(fmt.Sprintf("topic_config_log rows after a no-change register = %d, want 1", count))
	}
	fmt.Printf("  ✓ re-register resolved same id=%d, no mismatch, nothing appended\n", again.Id)

	step("re-register DIFFERENT config replaces the stored mutable config")
	redeclared, err := client.RegisterTopic(ctx, name, &vulkan.TopicConfig{RetentionTTL: 168 * time.Hour})
	must(err)
	if redeclared.Id != created.Id {
		die(fmt.Sprintf("re-declare resolved a different id: got %d, want %d", redeclared.Id, created.Id))
	}
	if redeclared.RetentionTTL != 168*time.Hour {
		die(fmt.Sprintf("re-declared RetentionTTL = %v, want 168h", redeclared.RetentionTTL))
	}
	if count := topicLogCount(ctx, ds, created.Id); count != 2 {
		die(fmt.Sprintf("topic_config_log rows after a config change = %d, want 2", count))
	}
	fmt.Printf("  ✓ newest declaration won: retention now %v on the same id=%d, snapshot appended\n", redeclared.RetentionTTL, redeclared.Id)

	step("re-register DIFFERENT PartitionSize is rejected")
	_, err = client.RegisterTopic(ctx, name, &vulkan.TopicConfig{RetentionTTL: 168 * time.Hour, PartitionSize: created.PartitionSize + 1})
	if !errors.Is(err, topic.ErrTopicConfigMismatch) {
		die(fmt.Sprintf("re-register with a different PartitionSize must return ErrTopicConfigMismatch, got: %v", err))
	}
	fmt.Printf("  ✓ changed PartitionSize rejected with ErrTopicConfigMismatch\n")

	fmt.Printf("\n✅ register idempotency lab PASSED\n")
	return nil
}

// topicLogCount reads the topic's trail directly -- machinery never reads
// topic_config_log, so the lab asserts on the table itself.
func topicLogCount(ctx context.Context, ds *iDatastore.PostgresDatastore, topicId int64) int {
	var count int
	must(ds.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM topic_config_log WHERE topic_id = $1;`, topicId).Scan(&count))
	return count
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
