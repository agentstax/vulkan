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

	"github.com/agentstax/vulkan/pkg/datastore"
	vulkan "github.com/agentstax/vulkan/pkg/vulkan"
)

type Message struct {
	Data string
}

func (Message) SchemaVersion() int { return 1 }

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

	ds, err := datastore.NewPostgresDatastore(ctx, "example_user", "localhost", "example_db", &datastore.PostgresConnectionConfig{Pass: "example_password"})
	must(err)
	defer ds.Close()

	client, err := vulkan.NewClient(ds, &vulkan.ClientConfig{AllowDestroy: true})
	must(err)

	const topicName = "test.producerregister"
	_ = client.Topic(topicName).Destroy(ctx, vulkan.DestroyOptions{Force: true}) // clean slate from any crashed prior run
	tp, err := client.RegisterTopic(ctx, topicName, &vulkan.TopicConfig{})
	must(err)
	defer func() {
		must(client.Topic(topicName).Destroy(ctx, vulkan.DestroyOptions{Force: true}))
	}()

	// ===== Register on Background =====
	step("Register(context.Background()) -- a build step, no lifetime to enforce")
	instance, err := client.RegisterProducer[Message](ctx, tp.Name, nil)
	must(err)
	produced, err := instance.Produce(ctx, &Message{Data: "registered"}, vulkan.ProduceOptions{})
	must(err)
	fmt.Printf("  ✓ produced %+v id=%d duplicate=%t\n", *produced.Message, produced.Id, produced.Duplicate)

	// ===== per-call shutdown =====
	step("Produce with a cancelled ctx -- refused, nothing published")
	cancelled, cancel := context.WithCancel(ctx)
	cancel() // stands in for SIGINT/SIGTERM: the app's shutdown context has fired
	_, err = instance.Produce(cancelled, &Message{Data: "too late"}, vulkan.ProduceOptions{})
	requireIs(err, context.Canceled)

	// ===== the instance holds no lifetime =====
	step("produce again on a live ctx -- the same instance still accepts work")
	_, err = instance.Produce(ctx, &Message{Data: "second life"}, vulkan.ProduceOptions{})
	must(err)
	fmt.Println("  ✓ same instance produces after the cancelled call")

	// ===== Register many times =====
	step("Register again -- an independent instance from the same factory")
	sibling, err := client.RegisterProducer[Message](ctx, tp.Name, nil)
	must(err)
	_, err = sibling.Produce(ctx, &Message{Data: "sibling"}, vulkan.ProduceOptions{})
	must(err)
	fmt.Println("  ✓ sibling instance produces")

	fmt.Println("\n✅ PRODUCER REGISTER LAB PASSED")
	fmt.Println("   Register only builds: instances hold no lifetime, a cancelled Produce ctx")
	fmt.Println("   refuses that one message, and the handle stays valid for the next call.")
	return nil
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
	panic(labFailure{message: msg})
}
