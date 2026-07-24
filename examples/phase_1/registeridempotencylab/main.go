package main

// register idempotency lab: re-registering a topic is idempotent, and a
// conflicting config is rejected -- guarding the struct-equality check in
// topic.upsertTopic. created_at/updated_at are db-assigned and a Config carries
// neither, so the existing-topic branch threads the stored row's timestamps
// into ToTopic before the *found != *want compare. Forget that and every
// re-register would falsely report a config mismatch -- this lab is the tripwire.
//
// Confirms:
//  1. first Register creates the topic and stamps created_at == updated_at
//     (no alter path yet, so they start equal).
//  2. re-registering the SAME config resolves to the same topic, no error --
//     NOT a mismatch (the timestamp edge).
//  3. re-registering a DIFFERENT config returns ErrTopicConfigMismatch.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/agentstax/vulkan/pkg/admin"
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
	must(mAdmin.RegisterSystem(ctx))

	name := fmt.Sprintf("registeridempotency.lab.%d", time.Now().UnixNano())

	step("first register creates the topic")
	created, err := mAdmin.RegisterTopic(ctx, name, &topic.Config{RetentionTTL: 720 * time.Hour})
	must(err)
	defer func() { must(mAdmin.DestroyTopic(ctx, name, admin.DestroyOptions{Force: true})) }()
	if created.CreatedAt.IsZero() {
		die("created_at was not populated on register")
	}
	if !created.CreatedAt.Equal(created.UpdatedAt) {
		die(fmt.Sprintf("fresh topic should have created_at == updated_at, got %s vs %s", created.CreatedAt, created.UpdatedAt))
	}
	fmt.Printf("  ✓ created id=%d, created_at == updated_at (%s)\n", created.Id, created.CreatedAt.Format(time.RFC3339))

	step("re-register SAME config is idempotent, not a mismatch")
	// Fresh Config with the identical caller-set field -- RegisterTopic mutates
	// what it's given via WithDefaults, so don't reuse the first one.
	again, err := mAdmin.RegisterTopic(ctx, name, &topic.Config{RetentionTTL: 720 * time.Hour})
	if err != nil {
		die(fmt.Sprintf("re-register with identical config must succeed, got: %v", err))
	}
	if again.Id != created.Id {
		die(fmt.Sprintf("re-register resolved a different id: got %d, want %d", again.Id, created.Id))
	}
	fmt.Printf("  ✓ re-register resolved same id=%d, no mismatch\n", again.Id)

	step("re-register DIFFERENT config is rejected")
	_, err = mAdmin.RegisterTopic(ctx, name, &topic.Config{RetentionTTL: 168 * time.Hour})
	if !errors.Is(err, topic.ErrTopicConfigMismatch) {
		die(fmt.Sprintf("re-register with different config must return ErrTopicConfigMismatch, got: %v", err))
	}
	fmt.Printf("  ✓ different config rejected with ErrTopicConfigMismatch\n")

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
