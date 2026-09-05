// Command managerautorunlab proves the deployment's upkeep rides on Consume.
//
// Registers its own topic (destroyed on exit) and runs consumers against it
// while watching the system manager's own worker row. Consume runs the
// system manager beside the session, and the row is declared
// target_instances = 1, so the claim gate is what decides who reconciles --
// no leader election, no hand-wired RunManager.
//
// Confirms: two sessions on one client -> ONE live manager instance, with
// the survivor taking the claim over on RetryDelay and the last one out
// releasing it; a second process takes it over the same way; target_instances = 0 suspends upkeep deployment-wide and
// says so with VK0035; DisableManager runs none at all; and an explicit
// RunManager beside a Consume is still one claim, with the session covering
// upkeep once the explicit call stops.
package main

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/agentstax/vulkan/examples/phase_1/common"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	vulkan "github.com/agentstax/vulkan/pkg/vulkan"
)

// the manager re-claims a declined row on RunnerConfig.RetryDelay (30s
// jittered), so every takeover assertion polls rather than sleeping once
const takeoverWindow = 50 * time.Second

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

	pool, err := vulkan.NewPostgresPool(ctx, "example_user", "example_password", "localhost", "example_db", nil)
	must(err)
	defer pool.Close()

	client, err := vulkan.NewClient(ctx, pool, &vulkan.ClientConfig{AllowDestroy: true})
	must(err)
	ds := client.Datastore()

	topicName := fmt.Sprintf("managerautorunlab.%d", time.Now().UnixNano())
	_, err = client.Topic[common.Work](topicName).Register(ctx, nil)
	must(err)
	defer func() {
		must(client.Topic[common.Work](topicName).Destroy(ctx, &vulkan.DestroyOptions{Force: true}))
	}()

	// the row is the lab's whole subject -- a stale installation still
	// carries the old -1 and every count below would be meaningless
	step("PRECONDITION: the system manager row is declared target_instances = 1")
	target := scalar(ctx, ds, fmt.Sprintf(`SELECT target_instances FROM %s.worker_config WHERE name='manager' AND system_id IS NOT NULL`, ds.Schema))
	fmt.Printf("  target_instances=%d\n", target)
	if target != 1 {
		die(fmt.Sprintf("system manager row has target_instances=%d, want 1 -- re-declaration only updates metadata, so an installation created before the gate needs a drop+recreate of its schema", target))
	}

	// ===== phase 1: two sessions, one claim =====
	step("PHASE 1: two Consume sessions on one client -- one live manager, released when both end")
	firstSession := start(ctx, client, topicName, "managerautorunlab-a")
	secondSession := start(ctx, client, topicName, "managerautorunlab-b")
	assertLive(ctx, ds, "two sessions claim one manager between them", 1)

	firstSession.stop()
	waitLive(ctx, ds, "the remaining session takes the claim over", 1, takeoverWindow)
	secondSession.stop()
	assertLive(ctx, ds, "the last session out releases the claim", 0)

	// ===== phase 2: a second process takes over =====
	step("PHASE 2: the claim moves to another process when its holder leaves")
	holder := start(ctx, client, topicName, "managerautorunlab-a")
	assertLive(ctx, ds, "the first process claims", 1)

	secondClient, err := vulkan.NewClient(ctx, pool, nil)
	must(err)
	waiting := start(ctx, secondClient, topicName, "managerautorunlab-b")
	assertLive(ctx, ds, "the second process is declined -- still one claim", 1)

	holder.stop()
	waitLive(ctx, ds, "the waiting process took the claim over", 1, takeoverWindow)
	waiting.stop()
	assertLive(ctx, ds, "released again", 0)

	// ===== phase 3: the row is the deployment's dial =====
	step("PHASE 3: target_instances = 0 suspends upkeep deployment-wide (VK0035)")
	setTarget(ctx, ds, 0)
	defer setTarget(context.Background(), ds, 1)

	capture := newCaptureLogger()
	suspendedClient, err := vulkan.NewClient(ctx, pool, &vulkan.ClientConfig{Logger: capture})
	must(err)
	suspended := start(ctx, suspendedClient, topicName, "managerautorunlab-a")
	assertLive(ctx, ds, "a suspended row runs no manager", 0)
	if count := capture.countCode("warn", "VK0035"); count < 1 {
		die(fmt.Sprintf("the suspended session logged %d VK0035 warns, want >= 1", count))
	}
	fmt.Printf("  ✓ VK0035 warned %d time(s) -- the operator hears why\n", capture.countCode("warn", "VK0035"))
	suspended.stop()

	setTarget(ctx, ds, 1)

	// ===== phase 4: the opt-out =====
	step("PHASE 4: DisableManager runs no manager at all")
	disabledClient, err := vulkan.NewClient(ctx, pool, &vulkan.ClientConfig{DisableManager: true})
	must(err)
	disabled := start(ctx, disabledClient, topicName, "managerautorunlab-a")
	assertLive(ctx, ds, "a DisableManager session runs no manager", 0)
	disabled.stop()

	// ===== phase 5: an explicit run beside a session =====
	step("PHASE 5: an explicit RunManager beside a Consume is still one claim")
	explicitCtx, stopExplicit := context.WithCancel(ctx)
	explicitDone := make(chan error, 1)
	go func() { explicitDone <- client.Manager().Run(explicitCtx) }()
	session := start(ctx, client, topicName, "managerautorunlab-a")
	assertLive(ctx, ds, "the explicit run and the session share one claim", 1)

	stopExplicit()
	must(<-explicitDone)
	waitLive(ctx, ds, "the session carries upkeep once the explicit run stops", 1, takeoverWindow)
	session.stop()
	assertLive(ctx, ds, "released with the session", 0)

	fmt.Println("\n✅ MANAGER AUTO-RUN LAB PASSED")
	fmt.Println("   Consume carries the deployment's upkeep, the row's target_instances")
	fmt.Println("   arbitrates one reconcile loop, and DisableManager is the opt-out.")
	return nil
}

// runningSession is one Consume call's lifecycle handle: cancel stops it,
// done yields Consume's return.
type runningSession struct {
	cancel context.CancelFunc
	done   chan error
}

func (s *runningSession) stop() {
	s.cancel()
	must(<-s.done)
	// a released row is gone at once, but the release lands after Consume's
	// own return -- give the manager's drain a moment to finish
	time.Sleep(2 * time.Second)
}

func start(ctx context.Context, client *vulkan.Client, topicName string, group string) *runningSession {
	lifecycleCtx, cancel := context.WithCancel(ctx)
	instance, err := client.Topic[common.Work](topicName).Group(group).Register(lifecycleCtx, nil)
	must(err)

	done := make(chan error, 1)
	go func() {
		done <- instance.Consume(lifecycleCtx, func(ctx context.Context, work *common.Work) error { return nil }, nil)
	}()
	fmt.Printf("  session %q started\n", group)

	// the claim is one insert behind Consume's own startup
	time.Sleep(5 * time.Second)
	return &runningSession{cancel: cancel, done: done}
}

// liveManagers is the count of unexpired instances on the SYSTEM's manager
// row -- a group's manager rows are unbounded and are not this lab's subject.
func liveManagers(ctx context.Context, ds *iDatastore.PostgresDatastore) int64 {
	return scalar(ctx, ds, fmt.Sprintf(`
		SELECT count(*)
		FROM %s.worker_instance i
		JOIN %s.worker_config w ON w.id = i.worker_id
		WHERE w.name = 'manager'
			AND w.system_id IS NOT NULL
			AND i.expires_at > now()`, ds.Schema, ds.Schema))
}

func assertLive(ctx context.Context, ds *iDatastore.PostgresDatastore, label string, want int64) {
	got := liveManagers(ctx, ds)
	if got != want {
		die(fmt.Sprintf("%s: %d live system managers, want %d", label, got, want))
	}
	fmt.Printf("  ✓ %s (live=%d)\n", label, got)
}

// waitLive polls until the count is want, for the claim retry to land.
func waitLive(ctx context.Context, ds *iDatastore.PostgresDatastore, label string, want int64, within time.Duration) {
	started := time.Now()
	deadline := started.Add(within)
	for {
		got := liveManagers(ctx, ds)
		if got == want {
			fmt.Printf("  ✓ %s (live=%d after %s)\n", label, got, time.Since(started).Round(time.Second))
			return
		}
		if time.Now().After(deadline) {
			die(fmt.Sprintf("%s: %d live system managers after %s, want %d", label, got, within, want))
		}
		time.Sleep(time.Second)
	}
}

func setTarget(ctx context.Context, ds *iDatastore.PostgresDatastore, target int) {
	_, err := ds.Pool.Exec(ctx, fmt.Sprintf(`UPDATE %s.worker_config SET target_instances=$1 WHERE name='manager' AND system_id IS NOT NULL`, ds.Schema), target)
	must(err)
	fmt.Printf("  target_instances set to %d\n", target)
}

func scalar(ctx context.Context, ds *iDatastore.PostgresDatastore, sql string, args ...any) int64 {
	var value int64
	must(ds.Pool.QueryRow(ctx, sql, args...).Scan(&value))
	return value
}

// captureLogger counts declared codes by level -- labs assert on events by
// level and attribute, never by matching message text.
type captureLogger struct {
	mutex sync.Mutex
	lines []capturedLine
}

type capturedLine struct {
	level string
	args  map[string]any
}

func newCaptureLogger() *captureLogger {
	return &captureLogger{}
}

func (c *captureLogger) record(level string, args []any) {
	fields := map[string]any{}
	for i := 0; i+1 < len(args); i += 2 {
		if key, ok := args[i].(string); ok {
			fields[key] = args[i+1]
		}
	}
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.lines = append(c.lines, capturedLine{level: level, args: fields})
}

func (c *captureLogger) DebugContext(ctx context.Context, message string, args ...any) {
	c.record("debug", args)
}

func (c *captureLogger) InfoContext(ctx context.Context, message string, args ...any) {
	c.record("info", args)
}

func (c *captureLogger) WarnContext(ctx context.Context, message string, args ...any) {
	c.record("warn", args)
}

func (c *captureLogger) ErrorContext(ctx context.Context, message string, args ...any) {
	c.record("error", args)
}

// countCode is the number of lines at level carrying code=code.
func (c *captureLogger) countCode(level string, code string) int {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	matches := 0
	for _, line := range c.lines {
		if line.level == level && line.args["code"] == code {
			matches++
		}
	}
	return matches
}

func step(s string) { fmt.Printf("\n--- %s ---\n", s) }

func must(err error) {
	if err != nil {
		die(err.Error())
	}
}

func die(message string) {
	panic(labFailure{message: message})
}
