package main

// producer register lab: the producer lifecycle end to end.
//
// Confirms: Register rejects a context that can never be cancelled unless
// DisableGracefulShutdown declares fire-and-forget on purpose; the produce
// gate refuses work before Register (ErrNotRegistered) and after the
// lifecycle context is cancelled (ErrShutdownRequested), while the call's
// own context stays irrelevant to both; and registration is once per
// instance -- a second Register errors, wound down or not.

import (
	"context"
	"errors"
	"fmt"
	"os"

	vulkanctx "github.com/agentstax/vulkan/pkg/context"
	vulkanerrors "github.com/agentstax/vulkan/pkg/errors"
	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/topic"
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

	const topicName = "test.producerregister"
	_ = topic.Destroy(ctx, ds, topicName) // clean slate from any crashed prior run
	tp, err := topic.Register(ctx, ds, &topic.Config{Name: topicName})
	must(err)
	defer func() { must(topic.Destroy(ctx, ds, topicName)) }()

	p, err := producer.NewMessageProducer[Message](tp, ds, nil)
	must(err)

	// ===== produce before Register =====
	step("produce before Register -- expect ErrNotRegistered")
	_, err = p.Produce(ctx, &Message{Data: "too early"}, producer.ProduceOptions{})
	requireIs(err, vulkanerrors.ErrNotRegistered)

	// ===== non-cancellable lifecycle context =====
	step("Register(context.Background()) without opting out -- expect the teaching error")
	err = p.Register(context.Background())
	requireIs(err, vulkanerrors.ErrLifecycleContextNotCancellable)

	// ===== the graceful path =====
	step("Register with the real lifecycle context, then produce")
	lifecycle, stop := vulkanctx.LifecycleContext()
	defer stop()
	must(p.Register(lifecycle))
	work, err := p.Produce(ctx, &Message{Data: "registered"}, producer.ProduceOptions{})
	must(err)
	fmt.Printf("  ✓ produced %+v\n", *work)

	// ===== wind-down =====
	step("cancel the lifecycle context -- expect ErrShutdownRequested, call ctx untouched")
	stop() // stands in for SIGINT/SIGTERM: cancels the lifecycle context
	_, err = p.Produce(ctx, &Message{Data: "too late"}, producer.ProduceOptions{})
	requireIs(err, vulkanerrors.ErrShutdownRequested)

	// ===== registration is once per instance =====
	step("Register again after wind-down -- expect ErrAlreadyRegistered")
	err = p.Register(ctx)
	requireIs(err, vulkanerrors.ErrAlreadyRegistered)

	// ===== fire-and-forget escape hatch =====
	step("fresh producer with DisableGracefulShutdown -- Background is accepted")
	ff, err := producer.NewMessageProducer[Message](tp, ds, &producer.MessageProducerConfig{DisableGracefulShutdown: true})
	must(err)
	must(ff.Register(context.Background()))
	_, err = ff.Produce(ctx, &Message{Data: "fire and forget"}, producer.ProduceOptions{})
	must(err)
	fmt.Println("  ✓ registered and produced on context.Background()")

	fmt.Println("\n✅ PRODUCER REGISTER LAB PASSED")
	fmt.Println("   Register owns the lifecycle: no produce before it, none after its context")
	fmt.Println("   cancels, one registration per instance -- and fire-and-forget is a declared")
	fmt.Println("   choice, never a silent default.")
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
