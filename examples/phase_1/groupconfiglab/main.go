package main

// group config declaration lab: RegisterConsumer stores the group's declared
// config on the group's consumer worker rows, and Consume reads the stored
// document back at start -- the newest declaration wins.
//
// Confirms:
//  1. RegisterConsumer writes a SPARSE document: only the declared fields
//     appear as keys on the message_consumer row's metadata.
//  2. a second declarer with a differing document replaces the stored one,
//     the VK0059 warn fires on the second declarer's logger, and every
//     replace appends a worker_config_log snapshot with declared_by.
//  3. an instance registered BEFORE the second declaration still consumes
//     under the stored (second) document -- Consume reads at start, so the
//     failing message dead-letters on the second declarer's retry budget,
//     not the first's.
//  4. a RUNNING instance picks up a redeclared document on its refresh
//     interval: a message sitting at attempt 2 of a 3-attempt budget gets
//     attempts 3, 4, and 5 once the budget moves to 5 -- no restart.

import (
	"context"
	"errors"
	"fmt"
	"github.com/agentstax/vulkan/pkg/topic"
	"os"
	"sync"
	"time"

	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	vulkan "github.com/agentstax/vulkan/pkg/vulkan"
)

type labMessage struct {
	N int `json:"n"`
}

func (labMessage) SchemaVersion() int { return 1 }

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

	pool, err := iDatastore.NewPostgresPool(ctx, "example_user", "example_password", "localhost", "example_db", nil)
	must(err)
	defer pool.Close()

	ds, err := iDatastore.NewPostgresDatastore(ctx, pool, nil)
	must(err)

	captureA := newCaptureLogger()
	clientA, err := vulkan.NewClient(ds, &vulkan.ClientConfig{AllowDestroy: true, Logger: captureA})
	must(err)
	captureB := newCaptureLogger()
	clientB, err := vulkan.NewClient(ds, &vulkan.ClientConfig{AllowDestroy: true, Logger: captureB})
	must(err)

	suffix := time.Now().UnixNano()
	topicName := fmt.Sprintf("groupconfiglab.%d", suffix)
	registered, err := clientA.RegisterTopic(ctx, topicName, nil)
	must(err)
	defer func() {
		if destroyErr := clientA.Topic(topicName).Destroy(ctx, &vulkan.DestroyOptions{Force: true}); destroyErr != nil {
			fmt.Printf("  cleanup: %s\n", destroyErr.Error())
		}
	}()
	group := fmt.Sprintf("groupconfiglab.group.%d", suffix)

	step("RegisterConsumer stores the declared config as a sparse document")
	instanceA, err := clientA.RegisterConsumer[labMessage](ctx, group, topicName, &vulkan.ConsumerConfig{
		Message:                 &vulkan.MessageOptions{Retry: &vulkan.RetryPolicy{MaxRetries: 5, BaseDelay: 100 * time.Millisecond}},
		ExceptionInitialBackoff: 200 * time.Millisecond,
	})
	must(err)

	var groupId int64
	must(ds.Pool.QueryRow(ctx, fmt.Sprintf(`SELECT id FROM %s.consumer_group_config WHERE topic_id = $1 AND name = $2;`, ds.Schema), registered.Id, group).Scan(&groupId))
	var hasMessage, hasBackoff, hasReclaims bool
	must(ds.Pool.QueryRow(ctx, fmt.Sprintf(`
		SELECT
			metadata ? 'message',
			metadata ? 'exception_initial_backoff',
			metadata ? 'max_range_reclaims'
		FROM %s.worker_config
		WHERE consumer_group_id = $1 AND name = 'message_consumer';`, ds.Schema), groupId).
		Scan(&hasMessage, &hasBackoff, &hasReclaims))
	if !hasMessage || !hasBackoff {
		die(fmt.Sprintf("stored document is missing declared keys: message=%t exception_initial_backoff=%t", hasMessage, hasBackoff))
	}
	if hasReclaims {
		die("stored document carries max_range_reclaims, which the declaration never set -- the document is not sparse")
	}
	fmt.Println("  ✓ declared keys stored, undeclared keys absent")

	step("a differing second declaration replaces the document and warns")
	_, err = clientB.RegisterConsumer[labMessage](ctx, group, topicName, &vulkan.ConsumerConfig{
		Message:                 &vulkan.MessageOptions{Retry: &vulkan.RetryPolicy{MaxRetries: 2, BaseDelay: 100 * time.Millisecond}},
		ExceptionInitialBackoff: 200 * time.Millisecond,
	})
	must(err)

	if count := captureB.countCode("warn", "VK0059"); count < 1 {
		die(fmt.Sprintf("second declarer logged %d VK0059 warns, want >= 1", count))
	}
	if count := captureA.countCode("warn", "VK0059"); count != 0 {
		die(fmt.Sprintf("first declarer logged %d VK0059 warns, want 0", count))
	}
	var storedMaxRetries string
	must(ds.Pool.QueryRow(ctx, fmt.Sprintf(`
		SELECT metadata->'message'->'retry'->>'max_retries'
		FROM %s.worker_config
		WHERE consumer_group_id = $1 AND name = 'message_consumer';`, ds.Schema), groupId).
		Scan(&storedMaxRetries))
	if storedMaxRetries != "2" {
		die(fmt.Sprintf("stored max_retries is %q, want \"2\" -- the newest declaration did not win", storedMaxRetries))
	}
	var logRows int
	must(ds.Pool.QueryRow(ctx, fmt.Sprintf(`
		SELECT COUNT(*)
		FROM %s.worker_config_log l
		JOIN %s.worker_config w ON w.id = l.worker_id
		WHERE w.consumer_group_id = $1 AND w.name = 'message_consumer' AND l.declared_by <> '';`, ds.Schema, ds.Schema), groupId).
		Scan(&logRows))
	if logRows != 2 {
		die(fmt.Sprintf("message_consumer has %d worker_config_log rows, want 2 (create + replace)", logRows))
	}
	fmt.Println("  ✓ VK0059 on the second declarer only; log snapshots carry declared_by")

	step("an instance registered before the replace consumes under the stored document")
	produced, err := clientA.RegisterProducer[labMessage](ctx, topicName, nil)
	must(err)
	_, err = produced.Produce(ctx, &labMessage{N: 1}, nil)
	must(err)

	consumeCtx, stopConsume := context.WithCancel(ctx)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = instanceA.Consume(consumeCtx, func(ctx context.Context, message *labMessage) error {
			return errors.New("groupconfiglab: always fails")
		}, &vulkan.ConsumeOptions{ClaimPollRate: 100 * time.Millisecond})
	}()

	var attempts int
	deadline := time.Now().Add(30 * time.Second)
	for {
		var status string
		err := ds.Pool.QueryRow(ctx, fmt.Sprintf(`SELECT status, attempts FROM %s.%s WHERE consumer_group_id = $1;`, ds.Schema, topic.ExceptionQueueTable(registered.Id)), groupId).Scan(&status, &attempts)
		if err == nil && status == "dead" {
			break
		}
		if time.Now().After(deadline) {
			stopConsume()
			wg.Wait()
			die("message never dead-lettered within 30s")
		}
		time.Sleep(100 * time.Millisecond)
	}
	stopConsume()
	wg.Wait()

	// the first declarer asked for 5 retries, the stored document says 2 --
	// a budget past 3 attempts means the instance ran on its process copy
	if attempts > 3 {
		die(fmt.Sprintf("dead after %d attempts -- the instance consumed under its own declaration, not the stored one", attempts))
	}
	fmt.Printf("  ✓ dead after %d attempts, the stored budget (declared 5, stored 2)\n", attempts)

	step("a running instance picks up a redeclared retry budget on refresh")
	liveGroup := fmt.Sprintf("groupconfiglab.live.%d", suffix)
	instanceLive, err := clientA.RegisterConsumer[labMessage](ctx, liveGroup, topicName, &vulkan.ConsumerConfig{
		// BaseDelay == MaxDelay flattens the backoff curve: every retry waits
		// 3s, so the redeclare below has a full 3s window to land and refresh
		Message:                 &vulkan.MessageOptions{Retry: &vulkan.RetryPolicy{MaxRetries: 3, BaseDelay: 3 * time.Second, MaxDelay: 3 * time.Second}},
		ExceptionInitialBackoff: 300 * time.Millisecond,
	})
	must(err)
	var liveGroupId int64
	must(ds.Pool.QueryRow(ctx, fmt.Sprintf(`SELECT id FROM %s.consumer_group_config WHERE topic_id = $1 AND name = $2;`, ds.Schema), registered.Id, liveGroup).Scan(&liveGroupId))
	_, err = produced.Produce(ctx, &labMessage{N: 2}, nil)
	must(err)

	liveCtx, stopLive := context.WithCancel(ctx)
	var liveWg sync.WaitGroup
	liveWg.Add(1)
	go func() {
		defer liveWg.Done()
		_ = instanceLive.Consume(liveCtx, func(ctx context.Context, message *labMessage) error {
			return errors.New("groupconfiglab: always fails")
		}, &vulkan.ConsumeOptions{ClaimPollRate: 100 * time.Millisecond, ConfigRefreshInterval: 200 * time.Millisecond})
	}()
	stopLiveConsumer := func() {
		stopLive()
		liveWg.Wait()
	}

	// wait for the message to sit at attempt 2 -- ready, inside its 3s backoff
	deadline = time.Now().Add(15 * time.Second)
	for {
		var status string
		var liveAttempts int
		err := ds.Pool.QueryRow(ctx, fmt.Sprintf(`SELECT status, attempts FROM %s.%s WHERE consumer_group_id = $1;`, ds.Schema, topic.ExceptionQueueTable(registered.Id)), liveGroupId).Scan(&status, &liveAttempts)
		if err == nil && liveAttempts >= 2 && status == "ready" {
			if liveAttempts > 2 {
				stopLiveConsumer()
				die(fmt.Sprintf("message already at attempt %d before the redeclare -- the backoff window was missed", liveAttempts))
			}
			break
		}
		if time.Now().After(deadline) {
			stopLiveConsumer()
			die("message never reached attempt 2 within 15s")
		}
		time.Sleep(50 * time.Millisecond)
	}

	// redeclare under the running instance: budget 3 -> 5, same backoff curve
	_, err = clientB.RegisterConsumer[labMessage](ctx, liveGroup, topicName, &vulkan.ConsumerConfig{
		Message:                 &vulkan.MessageOptions{Retry: &vulkan.RetryPolicy{MaxRetries: 5, BaseDelay: 3 * time.Second, MaxDelay: 3 * time.Second}},
		ExceptionInitialBackoff: 300 * time.Millisecond,
	})
	if err != nil {
		stopLiveConsumer()
		must(err)
	}

	deadline = time.Now().Add(30 * time.Second)
	for {
		var status string
		var liveAttempts int
		err := ds.Pool.QueryRow(ctx, fmt.Sprintf(`SELECT status, attempts FROM %s.%s WHERE consumer_group_id = $1;`, ds.Schema, topic.ExceptionQueueTable(registered.Id)), liveGroupId).Scan(&status, &liveAttempts)
		if err == nil && status == "dead" {
			stopLiveConsumer()
			// dead at 3 means the refresh never reached the running instance;
			// dead at 5 means attempts 4 and 5 ran under the redeclared budget
			if liveAttempts != 5 {
				die(fmt.Sprintf("dead after %d attempts, want 5 -- the running instance did not pick up the redeclared budget", liveAttempts))
			}
			break
		}
		if time.Now().After(deadline) {
			stopLiveConsumer()
			die("message never dead-lettered within 30s of the redeclare")
		}
		time.Sleep(50 * time.Millisecond)
	}
	fmt.Println("  ✓ attempts 4 and 5 ran under the budget redeclared mid-run (dead at 5, not 3)")

	fmt.Println("\n✅ GROUP CONFIG LAB PASSED")
	fmt.Println("   the declaration is stored sparse, the newest declaration wins with a")
	fmt.Println("   VK0059 warn, Consume reads the stored document back at start, and a")
	fmt.Println("   running instance follows a redeclaration on its refresh interval.")
	return nil
}

func step(title string) {
	fmt.Printf("\n== %s\n", title)
}

func must(err error) {
	if err != nil {
		die(err.Error())
	}
}

func die(message string) {
	panic(labFailure{message: message})
}

// captureLogger records every line so assertions can count warns by their
// code attribute.
type captureLogger struct {
	mu    sync.Mutex
	lines []capturedLine
}

type capturedLine struct {
	level   string
	message string
	args    map[string]any
}

func newCaptureLogger() *captureLogger {
	return &captureLogger{}
}

func (c *captureLogger) record(level string, message string, args []any) {
	fields := map[string]any{}
	for i := 0; i+1 < len(args); i += 2 {
		if key, ok := args[i].(string); ok {
			fields[key] = args[i+1]
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lines = append(c.lines, capturedLine{level: level, message: message, args: fields})
}

func (c *captureLogger) DebugContext(ctx context.Context, message string, args ...any) {
	c.record("debug", message, args)
}

func (c *captureLogger) InfoContext(ctx context.Context, message string, args ...any) {
	c.record("info", message, args)
}

func (c *captureLogger) WarnContext(ctx context.Context, message string, args ...any) {
	c.record("warn", message, args)
}

func (c *captureLogger) ErrorContext(ctx context.Context, message string, args ...any) {
	c.record("error", message, args)
}

// countCode is the number of lines at level carrying code=code.
func (c *captureLogger) countCode(level string, code string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	matches := 0
	for _, line := range c.lines {
		if line.level == level && line.args["code"] == code {
			matches++
		}
	}
	return matches
}
