package main

// binding lifecycle lab: a group's set is declared at Register and replaced
// only when no live instance still declares it.
//
// Registers its own topic, destroyed on exit. Drives the consumer API end to
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

	"github.com/agentstax/vulkan/pkg/consumergroup"
	consumergroupjanitorcontroller "github.com/agentstax/vulkan/pkg/consumergroup/janitor/controller"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	vulkan "github.com/agentstax/vulkan/pkg/vulkan"
)

type labMessage struct {
	Note string
}

func (labMessage) SchemaVersion() int { return 1 }

const groupName = "bindinglab.group"

var (
	ds        *iDatastore.PostgresDatastore
	client    *vulkan.Client
	topicName string
	topicId   int64
	groupId   int64
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

	ds, err = iDatastore.NewPostgresDatastore(ctx, "example_user", "localhost", "example_db", &iDatastore.PostgresConnectionConfig{Pass: "example_password"})
	must(err)
	defer ds.Close()

	client, err = vulkan.NewClient(ds, &vulkan.ClientConfig{AllowDestroy: true})
	must(err)

	topicName = fmt.Sprintf("bindinglab.%d", time.Now().UnixNano())
	registered, err := client.RegisterTopic(ctx, topicName, nil)
	must(err)
	topicId = registered.Id
	defer func() {
		must(client.Topic(topicName).Destroy(ctx, &vulkan.DestroyOptions{Force: true}))
	}()

	// ===== install + join =====
	step("Register declares the set; a same-set Register joins without writing")
	incumbent, err := registerConsumer(ctx, []string{"orders.*"})
	must(err)
	must(ds.Pool.QueryRow(ctx, `SELECT id FROM consumer_group_config WHERE topic_id = $1 AND name = $2;`,
		registered.Id, groupName).Scan(&groupId))
	assertInt("one installed row", installedRows(ctx), 1)
	assertString("binding rows", bindingDisplays(ctx), "orders.*")

	_, err = registerConsumer(ctx, []string{"orders.*"})
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

	divergent, err := registerConsumer(ctx, []string{"payments.*"})
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

	wpInstance, err := client.RegisterProducer[labMessage](ctx, topicName, nil)
	must(err)
	_, err = wpInstance.Produce(ctx, &labMessage{Note: "charged"}, &vulkan.ProduceOptions{RoutingKey: "payments.charge"})
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
		fmt.Sprintf(`UPDATE binding_config_log_%d SET attempted_at = attempted_at - interval '8 days' WHERE consumer_group_id = $1;`, topicId),
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
		fmt.Sprintf(`SELECT id FROM binding_config_log_%d WHERE consumer_group_id = $1 AND declared_by = 'bindinglab.dead-declarer';`, topicId),
		groupId).Scan(&survivingSyntheticId))
	if survivingSyntheticId != syntheticNewestId {
		die(fmt.Sprintf("the dead declarer's newest waiting row must survive: got id %d, want %d", survivingSyntheticId, syntheticNewestId))
	}

	swept, err = sweepController.SweepExpiredWaitingDeclarations(ctx, 7*24*time.Hour, 1000)
	must(err)
	assertInt("a second sweep deletes nothing", int(swept), 0)
	fmt.Println("  ✓ superseded rows swept; each declarer's newest waiting row and all installed rows kept")

	fmt.Println("\n✅ BINDING LAB PASSED")
	return nil
}

// registerConsumer declares the lab group's set with the lab's tight
// heartbeat and retry knobs.
func registerConsumer(ctx context.Context, bindings []string) (*vulkan.ConsumerInstance[labMessage], error) {
	return client.RegisterConsumer[labMessage](ctx, groupName, topicName, bindings, &vulkan.ConsumerConfig{
		ClaimPollRate:        500 * time.Millisecond,
		InstanceTTL:          2 * time.Second,
		BindingRetryInterval: 300 * time.Millisecond,
	})
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
				JOIN worker_config ON worker_config.id = worker_instance.worker_id
				WHERE worker_config.consumer_group_id = $1 AND worker_instance.expires_at > now()
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
		must(ds.Pool.QueryRow(ctx, fmt.Sprintf(`
			INSERT INTO binding_config_log_%d (consumer_group_id, status, patterns, declared_by, declared_at)
			VALUES ($1, 'waiting', '{"refunds.*"}', 'bindinglab.dead-declarer', now())
			RETURNING id;`, topicId), groupId).Scan(&newestId))
	}
	return newestId
}

func installedRows(ctx context.Context) int {
	var count int
	must(ds.Pool.QueryRow(ctx,
		fmt.Sprintf(`SELECT COUNT(*) FROM binding_config_log_%d WHERE consumer_group_id = $1 AND status = 'installed';`, topicId),
		groupId).Scan(&count))
	return count
}

func waitingRows(ctx context.Context) int {
	var count int
	must(ds.Pool.QueryRow(ctx,
		fmt.Sprintf(`SELECT COUNT(*) FROM binding_config_log_%d WHERE consumer_group_id = $1 AND status = 'waiting';`, topicId),
		groupId).Scan(&count))
	return count
}

func bindingDisplays(ctx context.Context) string {
	var displays string
	must(ds.Pool.QueryRow(ctx,
		fmt.Sprintf(`SELECT COALESCE(string_agg(pattern, ',' ORDER BY pattern), '') FROM binding_config_%d WHERE consumer_group_id = $1;`, topicId),
		groupId).Scan(&displays))
	return displays
}

// labDeclarations reads the listing surface and returns the lab group's
// installed row and open waiting row (nil when absent).
func labDeclarations(ctx context.Context) (*consumergroup.Declaration, *consumergroup.Declaration) {
	declarations, err := client.ListDeclarations(ctx)
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

func must(err error) {
	if err != nil {
		die(err.Error())
	}
}

func die(msg string) {
	panic(labFailure{message: msg})
}
