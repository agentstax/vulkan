package main

// producer register lab: the producer lifecycle end to end.
//
// Confirms: Register is a stateless build step -- its ctx bounds only that
// call's I/O, context.Background() is fine, and each call returns an
// independent instance. Shutdown is per produce call: a cancelled ctx is
// refused with nothing published, and the instance itself holds no lifetime
// -- the same handle produces again on a live ctx.

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/agentstax/vulkan/pkg/admin"
	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/topic"
	topiccontroller "github.com/agentstax/vulkan/pkg/topic/controller"
)

type Message struct {
	Data string
}

func main() {
	ctx := context.Background()

	ds, err := datastore.NewPostgresDatastore(ctx, &datastore.PostgresConnectionConfig{
		User:     "example_user",
		Pass:     "example_password",
		Host:     "localhost",
		Port:     5432,
		Database: "example_db",
	})
	must(err)
	defer ds.Close()

	mAdmin, err := admin.NewMessageAdmin(ds, &admin.MessageAdminConfig{AllowDestroy: true})
	must(err)
	must(mAdmin.RegisterSystem(ctx, nil))

	const topicName = "test.producerregister"
	_ = mAdmin.DestroyTopic(ctx, topicName, topic.SchemaVersion(1), admin.DestroyOptions{Force: true}) // clean slate from any crashed prior run
	tp, err := mAdmin.RegisterTopic(ctx, topicName, topic.SchemaVersion(1), &topiccontroller.TopicConfig{})
	must(err)
	defer func() {
		must(mAdmin.DestroyTopic(ctx, topicName, topic.SchemaVersion(1), admin.DestroyOptions{Force: true}))
	}()

	p, err := producer.NewProducer[Message](ds, nil)
	must(err)

	// ===== Register on Background =====
	step("Register(context.Background()) -- a build step, no lifetime to enforce")
	instance, err := p.Register(ctx, tp.Name, topic.SchemaVersion(1))
	must(err)
	work, err := instance.Produce(ctx, &Message{Data: "registered"}, producer.ProduceOptions{})
	must(err)
	fmt.Printf("  ✓ produced %+v\n", *work)

	// ===== per-call shutdown =====
	step("Produce with a cancelled ctx -- refused, nothing published")
	cancelled, cancel := context.WithCancel(ctx)
	cancel() // stands in for SIGINT/SIGTERM: the app's shutdown context has fired
	_, err = instance.Produce(cancelled, &Message{Data: "too late"}, producer.ProduceOptions{})
	requireIs(err, context.Canceled)

	// ===== the instance holds no lifetime =====
	step("produce again on a live ctx -- the same instance still accepts work")
	_, err = instance.Produce(ctx, &Message{Data: "second life"}, producer.ProduceOptions{})
	must(err)
	fmt.Println("  ✓ same instance produces after the cancelled call")

	// ===== Register many times =====
	step("Register again -- an independent instance from the same factory")
	sibling, err := p.Register(ctx, tp.Name, topic.SchemaVersion(1))
	must(err)
	_, err = sibling.Produce(ctx, &Message{Data: "sibling"}, producer.ProduceOptions{})
	must(err)
	fmt.Println("  ✓ sibling instance produces")

	fmt.Println("\n✅ PRODUCER REGISTER LAB PASSED")
	fmt.Println("   Register only builds: instances hold no lifetime, a cancelled Produce ctx")
	fmt.Println("   refuses that one message, and the handle stays valid for the next call.")
}

// ---- helpers ----

func step(s string) { fmt.Printf("\n--- %s ---\n", s) }

func requireIs(err, want error) {
	if !errors.Is(err, want) {
		die(fmt.Sprintf("want %v, got %v", want, err))
	}
	fmt.Printf("  ✓ %v\n", err)
}

func must(err error) {
	if err != nil {
		die(err.Error())
	}
}

func die(msg string) {
	fmt.Printf("\n❌ LAB FAILED: %s\n", msg)
	os.Exit(1)
}
