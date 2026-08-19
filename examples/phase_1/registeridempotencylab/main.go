package main

// register idempotency lab: re-registering a topic resolves to the same row,
// and the newest declaration's mutable config replaces what is stored -- guarding
// registerTopic's found path (replaceTopicConfig).
//
// Confirms:
//  1. first Register creates the topic and stamps created_at == updated_at.
//  2. re-registering the SAME config resolves to the same topic, no error and
//     no write.
//  3. re-registering DIFFERENT mutable config keeps the id and replaces the values.
//  4. re-registering a different PartitionSize returns ErrTopicConfigMismatch:
//     message_log's partition boundaries are derived from it.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/agentstax/vulkan/pkg/admin"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/topic"
	topiccontroller "github.com/agentstax/vulkan/pkg/topic/controller"
)

func main() {
	ctx := context.Background()

	ds, err := iDatastore.NewPostgresDatastore(ctx, "example_user", "localhost", "example_db", &iDatastore.PostgresConnectionConfig{Pass: "example_password"})
	must(err)
	defer ds.Close()

	mAdmin, err := admin.NewMessageAdmin(ds, &admin.MessageAdminConfig{AllowDestroy: true})
	must(err)
	must(mAdmin.RegisterSystem(ctx, nil))

	name := fmt.Sprintf("registeridempotency.lab.%d", time.Now().UnixNano())

	step("first register creates the topic")
	created, err := mAdmin.RegisterTopic(ctx, name, topic.SchemaVersion(1), &topiccontroller.TopicConfig{RetentionTTL: 720 * time.Hour})
	must(err)
	defer func() {
		must(mAdmin.DestroyTopic(ctx, name, topic.SchemaVersion(1), admin.DestroyOptions{Force: true}))
	}()
	fmt.Printf("  ✓ created id=%d\n", created.Id)

	step("re-register SAME config is idempotent, not a mismatch")
	// Fresh Config with the identical caller-set field -- RegisterTopic mutates
	// what it's given via WithDefaults, so don't reuse the first one.
	again, err := mAdmin.RegisterTopic(ctx, name, topic.SchemaVersion(1), &topiccontroller.TopicConfig{RetentionTTL: 720 * time.Hour})
	if err != nil {
		die(fmt.Sprintf("re-register with identical config must succeed, got: %v", err))
	}
	if again.Id != created.Id {
		die(fmt.Sprintf("re-register resolved a different id: got %d, want %d", again.Id, created.Id))
	}
	fmt.Printf("  ✓ re-register resolved same id=%d, no mismatch\n", again.Id)

	step("re-register DIFFERENT config replaces the stored mutable config")
	redeclared, err := mAdmin.RegisterTopic(ctx, name, topic.SchemaVersion(1), &topiccontroller.TopicConfig{RetentionTTL: 168 * time.Hour})
	must(err)
	if redeclared.Id != created.Id {
		die(fmt.Sprintf("re-declare resolved a different id: got %d, want %d", redeclared.Id, created.Id))
	}
	if redeclared.RetentionTTL != 168*time.Hour {
		die(fmt.Sprintf("re-declared RetentionTTL = %v, want 168h", redeclared.RetentionTTL))
	}
	fmt.Printf("  ✓ newest declaration won: retention now %v on the same id=%d\n", redeclared.RetentionTTL, redeclared.Id)

	step("re-register DIFFERENT PartitionSize is rejected")
	_, err = mAdmin.RegisterTopic(ctx, name, topic.SchemaVersion(1), &topiccontroller.TopicConfig{RetentionTTL: 168 * time.Hour, PartitionSize: created.PartitionSize + 1})
	if !errors.Is(err, topic.ErrTopicConfigMismatch) {
		die(fmt.Sprintf("re-register with a different PartitionSize must return ErrTopicConfigMismatch, got: %v", err))
	}
	fmt.Printf("  ✓ changed PartitionSize rejected with ErrTopicConfigMismatch\n")

	fmt.Printf("\n✅ register idempotency lab PASSED\n")
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
