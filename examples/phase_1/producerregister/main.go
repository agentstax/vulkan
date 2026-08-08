package main

// producer register lab: the producer lifecycle end to end.
//
// Confirms: Register rejects a context that can never be cancelled unless
// DisableGracefulShutdown declares fire-and-forget on purpose; Register's ctx
// is the instance's lifetime -- once it cancels, the produce gate refuses new
// work (ErrShutdownRequested) while the call's own context stays irrelevant;
// and Register is callable many times, so a wound-down instance is replaced
// by registering again, never by constructing a new Producer.

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/agentstax/vulkan/pkg/admin"
	vulkanctx "github.com/agentstax/vulkan/pkg/context"
	"github.com/agentstax/vulkan/pkg/datastore"
	vulkanerrors "github.com/agentstax/vulkan/pkg/errors"
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

	// ===== non-cancellable lifecycle context =====
	step("Register(context.Background()) without opting out -- expect the teaching error")
	_, err = p.Register(context.Background(), tp.Name, topic.SchemaVersion(1))
	requireIs(err, vulkanerrors.ErrLifecycleContextNotCancellable)

	// ===== the graceful path =====
	step("Register with the real lifecycle context, then produce")
	lifecycle, stop := vulkanctx.LifecycleContext(nil)
	defer stop()
	instance, err := p.Register(lifecycle, tp.Name, topic.SchemaVersion(1))
	must(err)
	work, err := instance.Produce(ctx, &Message{Data: "registered"}, producer.ProduceOptions{})
	must(err)
	fmt.Printf("  ✓ produced %+v\n", *work)

	// ===== wind-down =====
	step("cancel the lifecycle context -- expect ErrShutdownRequested, call ctx untouched")
	stop() // stands in for SIGINT/SIGTERM: cancels the lifecycle context
	_, err = instance.Produce(ctx, &Message{Data: "too late"}, producer.ProduceOptions{})
	requireIs(err, vulkanerrors.ErrShutdownRequested)

	// ===== Register again after wind-down =====
	step("Register again -- a fresh instance produces, the wound-down one stays down")
	lifecycle2, stop2 := vulkanctx.LifecycleContext(nil)
	defer stop2()
	replacement, err := p.Register(lifecycle2, tp.Name, topic.SchemaVersion(1))
	must(err)
	_, err = replacement.Produce(ctx, &Message{Data: "second life"}, producer.ProduceOptions{})
	must(err)
	fmt.Println("  ✓ replacement instance produces")
	_, err = instance.Produce(ctx, &Message{Data: "still too late"}, producer.ProduceOptions{})
	requireIs(err, vulkanerrors.ErrShutdownRequested)

	// ===== fire-and-forget escape hatch =====
	step("producer with DisableGracefulShutdown -- Background is accepted")
	ff, err := producer.NewProducer[Message](ds, &producer.ProducerConfig{DisableGracefulShutdown: true})
	must(err)
	ffInstance, err := ff.Register(context.Background(), tp.Name, topic.SchemaVersion(1))
	must(err)
	_, err = ffInstance.Produce(ctx, &Message{Data: "fire and forget"}, producer.ProduceOptions{})
	must(err)
	fmt.Println("  ✓ registered and produced on context.Background()")

	fmt.Println("\n✅ PRODUCER REGISTER LAB PASSED")
	fmt.Println("   Register's context owns each instance's lifecycle: no produce after it")
	fmt.Println("   cancels, a new Register replaces a wound-down instance, and fire-and-forget")
	fmt.Println("   is a declared choice, never a silent default.")
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
