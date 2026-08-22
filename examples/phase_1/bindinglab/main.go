package main

// binding lifecycle lab: a group's set is declared at Register and replaced
// only when no live instance still declares it.
//
// Registers its own topic, destroyed on exit. Drives the consumer door end to
// end -- real Register attempts, a real consuming incumbent whose heartbeats
// block the swap, and a real Consume blocked in its declaration wait.
//
// Confirms: a re-Register of the same set joins without writing; a divergent
// set against a live incumbent waits -- visibly, appending a waiting row per
// retry, never touching the effective set, never starting the manager; the
// declaration listing shows the effective row and the open waiter; stopping
// the incumbent (the rolling deploy's kill) lets the waiter install, swap the
// binding rows, and consume messages routed to its set; the ended wait
// disappears from the listing; the consumer group janitor's sweep deletes
// superseded waiting rows past the TTL while keeping each declarer's newest
// waiting row and every installed row.

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/agentstax/vulkan/pkg/admin"
	"github.com/agentstax/vulkan/pkg/consumer"
	"github.com/agentstax/vulkan/pkg/consumergroup"
	consumergroupjanitorcontroller "github.com/agentstax/vulkan/pkg/consumergroup/janitor/controller"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/topic"
)

type labMessage struct {
	Note string
}

const groupName = "bindinglab.group"

var (
	ds        *iDatastore.PostgresDatastore
	mAdmin    *admin.MessageAdmin
	topicName string
	groupId   int64
)

func main() {
	ctx := context.Background()

	var err error
	ds, err = iDatastore.NewPostgresDatastore(ctx, "example_user", "localhost", "example_db", &iDatastore.PostgresConnectionConfig{Pass: "example_password"})
	must(err)
	defer ds.Close()

	mAdmin, err = admin.NewMessageAdmin(ds, &admin.MessageAdminConfig{AllowDestroy: true})
	must(err)
	must(mAdmin.RegisterSystem(ctx, nil))

	topicName = fmt.Sprintf("bindinglab.%d", time.Now().UnixNano())
	registered, err := mAdmin.RegisterTopic(ctx, topicName, topic.SchemaVersion(1), nil)
	must(err)
	defer func() {
		must(mAdmin.DestroyTopic(ctx, topicName, topic.SchemaVersion(1), admin.DestroyOptions{Force: true}))
	}()

	// ===== install + join =====
	step("Register declares the set; a same-set Register joins without writing")
	incumbentConsumer := newConsumer()
	incumbent, err := incumbentConsumer.Register(ctx, groupName, topicName, topic.SchemaVersion(1), []string{"orders.*"})
	must(err)
	must(ds.Pool.QueryRow(ctx, `SELECT id FROM consumer_group WHERE topic_id = $1 AND name = $2;`,
		registered.Id, groupName).Scan(&groupId))
	assertInt("one installed row", installedRows(ctx), 1)
	assertString("binding rows", bindingDisplays(ctx), "orders.*")

	_, err = newConsumer().Register(ctx, groupName, topicName, topic.SchemaVersion(1), []string{"orders.*"})
	must(err)
	assertInt("still one installed row after the same set re-registers", installedRows(ctx), 1)
	fmt.Println("  ✓ installed once, joined on re-register")

	// ===== divergent set waits on the live incumbent =====
	step("a divergent set waits while the incumbent's heartbeats are fresh")
	incumbentCtx, stopIncumbent := context.WithCancel(ctx)
	incumbentDone := make(chan error, 1)
	go func() {
		incumbentDone <- incumbent.Consume(incumbentCtx, func(ctx context.Context, message *labMessage) error {
			return nil
		})
	}()
	waitLiveInstance(ctx)

	divergent, err := newConsumer().Register(ctx, groupName, topicName, topic.SchemaVersion(1), []string{"payments.*"})
	must(err)
	received := make(chan string, 1)
	divergentCtx, stopDivergent := context.WithCancel(ctx)
	divergentDone := make(chan error, 1)
	go func() {
		divergentDone <- divergent.Consume(divergentCtx, func(ctx context.Context, message *labMessage) error {
			select {
			case received <- message.Note:
			default:
			}
			return nil
		})
	}()

	// several retry intervals pass; the wait appends rows and changes nothing
	time.Sleep(1500 * time.Millisecond)
	select {
	case err := <-divergentDone:
		die(fmt.Sprintf("the divergent Consume must stay blocked, returned %v", err))
	default:
	}
	assertString("binding rows still the incumbent's", bindingDisplays(ctx), "orders.*")
	if got := waitingRows(ctx); got < 2 {
		die(fmt.Sprintf("want the wait re-appended as rows, got %d", got))
	}
	installed, waiter := labDeclarations(ctx)
	if installed == nil || patterns(installed) != "orders.*" {
		die("listing must show the incumbent's set as installed")
	}
	if waiter == nil || patterns(waiter) != "payments.*" {
		die("listing must show the divergent set as waiting")
	}
	fmt.Println("  ✓ waited: rows appended, effective set untouched, listing shows the open wait")

	// ===== the deploy kills the incumbent; the waiter converges =====
	step("stopping the incumbent lets the waiter install, swap, and consume")
	stopIncumbent()
	must(<-incumbentDone)

	deadline := time.Now().Add(30 * time.Second)
	for bindingDisplays(ctx) != "payments.*" {
		if time.Now().After(deadline) {
			die("the waiter never installed after the incumbent stopped")
		}
		time.Sleep(200 * time.Millisecond)
	}
	installed, waiter = labDeclarations(ctx)
	if installed == nil || patterns(installed) != "payments.*" {
		die("listing must show the swapped set as installed")
	}
	if waiter != nil {
		die("the ended wait must leave the listing")
	}
	fmt.Println("  ✓ swapped once the incumbent's heartbeats lapsed; wait left the listing")

	wp, err := producer.NewProducer[labMessage](ds, nil)
	must(err)
	wpInstance, err := wp.Register(ctx, topicName, topic.SchemaVersion(1))
	must(err)
	_, err = wpInstance.Produce(ctx, &labMessage{Note: "charged"}, producer.ProduceOptions{RoutingKey: "payments.charge"})
	must(err)
	select {
	case note := <-received:
		assertString("consumed under the new set", note, "charged")
	case <-time.After(30 * time.Second):
		die("the installed consumer never consumed a message routed to its set")
	}
	stopDivergent()
	must(<-divergentDone)
	fmt.Println("  ✓ consumed a message routed to the new set, stopped clean")

	// ===== retention: the janitor sweeps superseded waiting rows =====
	step("the consumer group janitor sweeps superseded waiting rows, keeping each declarer's newest")
	syntheticNewestId := insertSyntheticWaits(ctx)
	beforeSweep := waitingRows(ctx)
	_, err = ds.Pool.Exec(ctx,
		`UPDATE binding_log SET attempt_at = attempt_at - interval '8 days' WHERE consumer_group_id = $1;`,
		groupId)
	must(err)

	sweepController, err := consumergroupjanitorcontroller.NewJanitorController(ds, nil)
	must(err)
	swept, err := sweepController.SweepExpiredWaitingDeclarations(ctx, 7*24*time.Hour, 1000)
	must(err)
	assertInt("swept superseded waiting rows", int(swept), beforeSweep-2)
	assertInt("one waiting row per declarer survives past the TTL", waitingRows(ctx), 2)
	assertInt("installed rows untouched", installedRows(ctx), 2)

	var survivingSyntheticId int64
	must(ds.Pool.QueryRow(ctx,
		`SELECT id FROM binding_log WHERE consumer_group_id = $1 AND declared_by = 'bindinglab.dead-declarer';`,
		groupId).Scan(&survivingSyntheticId))
	if survivingSyntheticId != syntheticNewestId {
		die(fmt.Sprintf("the dead declarer's newest waiting row must survive: got id %d, want %d", survivingSyntheticId, syntheticNewestId))
	}

	swept, err = sweepController.SweepExpiredWaitingDeclarations(ctx, 7*24*time.Hour, 1000)
	must(err)
	assertInt("a second sweep deletes nothing", int(swept), 0)
	fmt.Println("  ✓ superseded rows swept; each declarer's newest waiting row and all installed rows kept")

	fmt.Println("\n✅ BINDING LAB PASSED")
}

func newConsumer() *consumer.Consumer[labMessage] {
	labConsumer, err := consumer.NewConsumer[labMessage](ds, &consumer.ConsumerConfig{
		ClaimPollRate:        500 * time.Millisecond,
		InstanceTTL:          2 * time.Second,
		BindingRetryInterval: 300 * time.Millisecond,
	})
	must(err)
	return labConsumer
}

// waitLiveInstance blocks until the consuming incumbent's heartbeat rows
// exist -- before that a divergent Register would install, not wait.
func waitLiveInstance(ctx context.Context) {
	deadline := time.Now().Add(30 * time.Second)
	for {
		var live bool
		must(ds.Pool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM worker_instance
				JOIN worker ON worker.id = worker_instance.worker_id
				WHERE worker.consumer_group_id = $1 AND worker_instance.expires_at > now()
			);`, groupId).Scan(&live))
		if live {
			return
		}
		if time.Now().After(deadline) {
			die("the incumbent never wrote a live worker_instance row")
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// insertSyntheticWaits appends three waiting rows for a declarer whose
// process is gone -- the dead waiter the sweep must keep visible. Returns
// the newest row's id.
func insertSyntheticWaits(ctx context.Context) int64 {
	var newestId int64
	for range 3 {
		must(ds.Pool.QueryRow(ctx, `
			INSERT INTO binding_log (consumer_group_id, status, patterns, declared_by, declared_at)
			VALUES ($1, 'waiting', '{"refunds.*"}', 'bindinglab.dead-declarer', now())
			RETURNING id;`, groupId).Scan(&newestId))
	}
	return newestId
}

func installedRows(ctx context.Context) int {
	var count int
	must(ds.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM binding_log WHERE consumer_group_id = $1 AND status = 'installed';`,
		groupId).Scan(&count))
	return count
}

func waitingRows(ctx context.Context) int {
	var count int
	must(ds.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM binding_log WHERE consumer_group_id = $1 AND status = 'waiting';`,
		groupId).Scan(&count))
	return count
}

func bindingDisplays(ctx context.Context) string {
	var displays string
	must(ds.Pool.QueryRow(ctx,
		`SELECT COALESCE(string_agg(display, ',' ORDER BY display), '') FROM binding WHERE consumer_group_id = $1;`,
		groupId).Scan(&displays))
	return displays
}

// labDeclarations reads the listing surface and returns the lab group's
// installed row and open waiting row (nil when absent).
func labDeclarations(ctx context.Context) (*consumergroup.Declaration, *consumergroup.Declaration) {
	declarations, err := mAdmin.ListDeclarations(ctx)
	must(err)
	var installed *consumergroup.Declaration
	var waiter *consumergroup.Declaration
	for _, declaration := range declarations {
		if declaration.TopicName != topicName || declaration.GroupName != groupName {
			continue
		}
		switch declaration.Status {
		case consumergroup.DeclarationInstalled:
			installed = declaration
		case consumergroup.DeclarationWaiting:
			waiter = declaration
		}
	}
	return installed, waiter
}

func patterns(declaration *consumergroup.Declaration) string {
	joined := ""
	for i, pattern := range declaration.Patterns {
		if i > 0 {
			joined += ","
		}
		joined += pattern
	}
	return joined
}

func assertInt(name string, got int, want int) {
	if got != want {
		die(fmt.Sprintf("%s: got %d, want %d", name, got, want))
	}
}

func assertString(name string, got string, want string) {
	if got != want {
		die(fmt.Sprintf("%s: got %q, want %q", name, got, want))
	}
}

func step(s string) { fmt.Printf("\n--- %s ---\n", s) }

func die(message string) {
	fmt.Fprintln(os.Stderr, "FAIL: "+message)
	os.Exit(1)
}

func must(err error) {
	if err != nil {
		die(err.Error())
	}
}
