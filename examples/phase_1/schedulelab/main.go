// Command schedulelab proves the schedule machinery end to end against a live
// scheduler worker claimed with a fast poll rate.
//
// Sections:
//  1. validation -- charset/star names, sub-minute and no-upcoming schedules,
//     timeout vs min rate, re-register wins, Feb-29 single-scheduled-time pass
//  2. target -- a schedule dies with its target topic; one targeting another
//     topic survives
//  2b. handle -- scheduler.Register declares the same row admin does, refuses
//     an unregistered topic and a bad expression, and Schedule runs the
//     system manager until its ctx cancels
//  3. produce-once -- a backdated row produces ONE message stamped with the
//     NEWEST due scheduled time, older dues dropped
//  4. v7 dedupe -- re-backdating to the SAME scheduled time is a Duplicate,
//     not a second message
//  5. suspend/unsuspend -- a suspended row never produces; a scheduled time
//     that came due while suspended is dropped, not produced late
//  6. poisoned row -- one job's produce fails every tick, siblings still
//     produce and the worker keeps ticking
//  7. exclusive (spot -- exclusivelab owns depth) -- a scheduler request lands while
//     a previous one is still running, waits, then runs
//  8. run-now beside a running request -- the default 'parallel' runs alongside
//     it; cfg.Concurrency exclusive waits for it instead
//  9. run-now supersedes a pending unclaimed request
//  10. consumer end-to-end + status -- bind the job's name, fail-once retry,
//     'success' rows land, ScheduleStatus shows RAN/SUCCEEDED/FAILED
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/agentstax/vulkan/pkg/admin"
	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/consumer"
	"github.com/agentstax/vulkan/pkg/consumergroup"
	consumergroupcontroller "github.com/agentstax/vulkan/pkg/consumergroup/controller"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/schedule"
	schedulecontroller "github.com/agentstax/vulkan/pkg/schedule/controller"
	scheduleproducer "github.com/agentstax/vulkan/pkg/schedule/producer"
	"github.com/agentstax/vulkan/pkg/scheduler"
	"github.com/agentstax/vulkan/pkg/topic"
	topiccontroller "github.com/agentstax/vulkan/pkg/topic/controller"
	"github.com/agentstax/vulkan/pkg/worker"
	workercontroller "github.com/agentstax/vulkan/pkg/worker/controller"
)

const schedulerPollRate = 100 * time.Millisecond

var (
	ds     *iDatastore.PostgresDatastore
	mAdmin *admin.MessageAdmin
	target *topic.Topic // the lab's own target topic
	prefix string
)

// labMessage is what every lab schedule produces.
type labMessage struct {
	Kind string `json:"kind"`
}

func (labMessage) SchemaVersion() int { return 1 }

var payload = &labMessage{Kind: "lab"}

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

	mAdmin, err = admin.NewMessageAdmin(ds, &admin.MessageAdminConfig{AllowDestroy: true})
	must(err)
	must(mAdmin.RegisterSystem(ctx, nil))

	prefix = fmt.Sprintf("schedulelab.%d", time.Now().UnixNano())
	// status reads count 'success' rows, so the target keeps every outcome
	target, err = mAdmin.RegisterTopic(ctx, prefix+".target", &topiccontroller.TopicConfig{DeliveryLogMode: topic.DeliveryLogModeAll})
	must(err)
	defer cleanupTarget()

	validationSection(ctx)
	targetSection(ctx)
	handleSection(ctx)

	// every scheduler-driven section below rides this one claimed instance
	stopScheduler := startScheduler(ctx)
	defer stopScheduler()

	produceOnceSection(ctx)
	dedupeSection(ctx)
	suspendSection(ctx)
	poisonSection(ctx)
	deferSection(ctx)
	runNowOverrideSection(ctx)
	supersedeSection(ctx)
	statusSection(ctx)

	fmt.Println("\n✅ SCHEDULE LAB PASSED")
	return nil
}

// --- sections ---

func validationSection(ctx context.Context) {
	step("validation: names, schedules, timeout vs min rate, re-register wins")

	hourly := parse("@hourly")

	if _, err := mAdmin.RegisterSchedule(ctx, "", hourly, target.Name, payload, nil); err == nil {
		die("empty name must be rejected")
	}
	if _, err := mAdmin.RegisterSchedule(ctx, prefix+".Upper", hourly, target.Name, payload, nil); err == nil {
		die("uppercase name must be rejected")
	}
	if _, err := mAdmin.RegisterSchedule(ctx, prefix+".star*", hourly, target.Name, payload, nil); err == nil {
		die("'*' in a name is the binding wildcard and must be rejected")
	}
	if _, err := schedule.ParseExpression("@every 30s"); err == nil {
		die("sub-minute expression must be rejected at parse")
	}
	// Feb 30 never exists, so the schedule has no upcoming scheduled time
	if _, err := schedule.ParseExpression("0 0 30 2 *"); err == nil {
		die("a expression with no upcoming scheduled time must be rejected at parse")
	}
	if _, err := mAdmin.RegisterSchedule(ctx, prefix+".validate", hourly, target.Name, payload, &schedulecontroller.ScheduleConfig{Timeout: 2 * time.Hour}); err == nil {
		die("timeout above the expression's min rate must be rejected")
	}
	fmt.Println("  ✓ rejections: empty/uppercase/star name, sub-minute, no-upcoming, timeout > min rate")

	// Feb-29 has under two scheduled times inside the min-rate horizon -- the
	// single-scheduled-time pass must register it, seeded on a real Feb 29
	feb29, err := mAdmin.RegisterSchedule(ctx, prefix+".feb29", parse("0 0 29 2 *"), target.Name, payload, nil)
	must(err)
	if feb29.NextScheduledAt.UTC().Month() != time.February || feb29.NextScheduledAt.UTC().Day() != 29 {
		die(fmt.Sprintf("feb29 job seeded to %v, want a Feb 29", feb29.NextScheduledAt))
	}
	must(mAdmin.DestroySchedule(ctx, prefix+".feb29"))
	fmt.Printf("  ✓ Feb-29 expression registered, seeded to %s\n", feb29.NextScheduledAt.UTC().Format("2006-01-02"))

	first, err := mAdmin.RegisterSchedule(ctx, prefix+".redeclare", hourly, target.Name, payload, nil)
	must(err)
	again, err := mAdmin.RegisterSchedule(ctx, prefix+".redeclare", hourly, target.Name, payload, nil)
	must(err)
	if again.Id != first.Id {
		die(fmt.Sprintf("identical re-register resolved to a different job: %d vs %d", again.Id, first.Id))
	}

	daily := parse("@daily")
	must(mAdmin.SuspendSchedule(ctx, prefix+".redeclare"))
	redeclared, err := mAdmin.RegisterSchedule(ctx, prefix+".redeclare", daily, target.Name, payload, nil)
	must(err)
	if redeclared.Id != first.Id {
		die(fmt.Sprintf("re-register resolved to a different job: %d vs %d", redeclared.Id, first.Id))
	}
	if redeclared.Expression != daily.String() {
		die(fmt.Sprintf("re-registered expression = %q, want %q", redeclared.Expression, daily))
	}
	if !redeclared.NextScheduledAt.After(time.Now().UTC()) {
		die(fmt.Sprintf("a expression change must re-seed the next scheduled time, got %v", redeclared.NextScheduledAt))
	}
	if !redeclared.Suspended {
		die("a re-register must leave a suspended job suspended")
	}
	must(mAdmin.DestroySchedule(ctx, prefix+".redeclare"))
	fmt.Println("  ✓ identical re-register is a no-op, a differing one wins and leaves suspended alone")
}

func targetSection(ctx context.Context) {
	step("target: a schedule dies with its target topic, one on another topic survives")

	topicName := prefix + ".ownedtopic"
	_, err := mAdmin.RegisterTopic(ctx, topicName, nil)
	must(err)

	_, err = mAdmin.RegisterSchedule(ctx, prefix+".cascade", parse("@hourly"), topicName, payload, nil)
	must(err)
	_, err = mAdmin.RegisterSchedule(ctx, prefix+".standalone", parse("@hourly"), target.Name, payload, nil)
	must(err)

	must(mAdmin.DestroyTopic(ctx, topicName, admin.DestroyOptions{Force: true}))

	cascaded, err := mAdmin.GetSchedule(ctx, prefix+".cascade")
	must(err)
	if cascaded != nil {
		die("a schedule must cascade away with its target topic")
	}
	standalone, err := mAdmin.GetSchedule(ctx, prefix+".standalone")
	must(err)
	if standalone == nil {
		die("a schedule on another topic must survive an unrelated topic destroy")
	}
	must(mAdmin.DestroySchedule(ctx, prefix+".standalone"))
	fmt.Println("  ✓ cascade removed the schedule with its target topic, the other survived")
}

func handleSection(ctx context.Context) {
	step("handle: scheduler.Register declares the row, Schedule runs the manager until ctx cancels")

	invoiceScheduler, err := scheduler.NewScheduler(ds, nil)
	must(err)

	if _, err := invoiceScheduler.Register[labMessage](ctx, prefix+".handle", "@hourly", prefix+".missing", payload, nil); !errors.Is(err, topic.ErrTopicNotFound) {
		die(fmt.Sprintf("want ErrTopicNotFound for an unregistered target, got %v", err))
	}
	if _, err := invoiceScheduler.Register[labMessage](ctx, prefix+".handle", "every day at noon", target.Name, payload, nil); err == nil {
		die("want an error for an unparseable expression")
	}

	nightly, err := invoiceScheduler.Register[labMessage](ctx, prefix+".handle", "@hourly", target.Name, payload, nil)
	must(err)
	defer func() { must(mAdmin.DestroySchedule(ctx, prefix+".handle")) }()
	found, err := mAdmin.GetSchedule(ctx, prefix+".handle")
	must(err)
	if found == nil || found.Id != nightly.Registered.Id || found.TopicId != target.Id || found.SchemaVersion != 1 {
		die(fmt.Sprintf("handle row differs from admin's read: %+v vs %+v", nightly.Registered, found))
	}
	if nightly.Payload != payload {
		die("instance must keep the payload it registered")
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	time.AfterFunc(2*time.Second, cancel)
	if err := nightly.Schedule(runCtx); err != nil {
		die(fmt.Sprintf("Schedule must return nil on a requested stop, got %v", err))
	}
	fmt.Println("  ✓ handle registered the same row admin reads; Schedule ran the manager and stopped clean")
}

func produceOnceSection(ctx context.Context) {
	step("produce-once: a 5m-backdated row produces ONE message, stamped with the NEWEST due scheduled time")

	job, err := mAdmin.RegisterSchedule(ctx, prefix+".walk", parse("@every 1m"), target.Name, payload, nil)
	must(err)
	defer func() { must(mAdmin.DestroySchedule(ctx, prefix+".walk")) }()
	key := job.Name

	backdated := time.Now().UTC().Add(-5 * time.Minute)
	backdate(ctx, job.Id, backdated)
	waitAdvanced(ctx, job.Id)

	if got := messageCount(ctx, key); got != 1 {
		die(fmt.Sprintf("want exactly 1 message for the backdated row, got %d", got))
	}
	produced := producedScheduledTimes(ctx, key)[0]
	if produced.Sub(backdated) < 3*time.Minute {
		die(fmt.Sprintf("produced scheduled time %v is too close to the backdate %v -- older dues were not dropped", produced, backdated))
	}
	if time.Since(produced) > 90*time.Second {
		die(fmt.Sprintf("produced scheduled time %v is not the newest due", produced))
	}
	fmt.Printf("  ✓ 1 message, scheduled time %v after the backdate (newest due)\n", produced.Sub(backdated).Round(time.Second))
}

func dedupeSection(ctx context.Context) {
	step("v7 dedupe: re-backdating to the SAME scheduled time is a Duplicate, not a second message")

	job, err := mAdmin.RegisterSchedule(ctx, prefix+".dedupe", parse("@every 1m"), target.Name, payload, nil)
	must(err)
	defer func() { must(mAdmin.DestroySchedule(ctx, prefix+".dedupe")) }()
	key := job.Name

	// 10s back: due now, and its successor stays out of reach for ~50s more,
	// so the re-backdated tick can only re-produce this exact scheduled time
	scheduledTime := time.Now().UTC().Add(-10 * time.Second)
	backdate(ctx, job.Id, scheduledTime)
	waitAdvanced(ctx, job.Id)
	if got := messageCount(ctx, key); got != 1 {
		die(fmt.Sprintf("setup: want 1 message, got %d", got))
	}

	backdate(ctx, job.Id, scheduledTime)
	waitAdvanced(ctx, job.Id)
	if got := messageCount(ctx, key); got != 1 {
		die(fmt.Sprintf("the same scheduled time produced twice: %d messages", got))
	}
	fmt.Println("  ✓ second tick on the same scheduled time deduped -- still 1 message")
}

func suspendSection(ctx context.Context) {
	step("suspend/unsuspend: a due-while-suspended scheduled time is dropped, not produced late")

	job, err := mAdmin.RegisterSchedule(ctx, prefix+".suspend", parse("@hourly"), target.Name, payload, nil)
	must(err)
	defer func() { must(mAdmin.DestroySchedule(ctx, prefix+".suspend")) }()
	key := job.Name

	must(mAdmin.SuspendSchedule(ctx, prefix+".suspend"))
	backdate(ctx, job.Id, time.Now().UTC().Add(-2*time.Hour))
	time.Sleep(10 * schedulerPollRate)
	if got := messageCount(ctx, key); got != 0 {
		die(fmt.Sprintf("suspended row produced %d messages", got))
	}

	must(mAdmin.UnsuspendSchedule(ctx, prefix+".suspend"))
	unsuspended, err := mAdmin.GetSchedule(ctx, prefix+".suspend")
	must(err)
	if !unsuspended.NextScheduledAt.After(time.Now()) {
		die(fmt.Sprintf("unsuspend must re-seed next_scheduled_at in the future, got %v", unsuspended.NextScheduledAt))
	}
	time.Sleep(5 * schedulerPollRate)
	if got := messageCount(ctx, key); got != 0 {
		die("the scheduled time that came due while suspended was produced late")
	}

	// positive control: the same row produces once genuinely due again
	backdate(ctx, job.Id, time.Now().UTC().Add(-2*time.Hour))
	waitAdvanced(ctx, job.Id)
	if got := messageCount(ctx, key); got != 1 {
		die(fmt.Sprintf("unsuspended row should produce when due, got %d messages", got))
	}
	fmt.Println("  ✓ suspended row silent, unsuspend re-seeded forward, due row produced again")
}

func poisonSection(ctx context.Context) {
	step("poisoned row: one job's produce fails every tick, siblings still produce")

	poisoned, err := mAdmin.RegisterSchedule(ctx, prefix+".poison", parse("@hourly"), target.Name, payload, nil)
	must(err)
	defer func() { must(mAdmin.DestroySchedule(ctx, prefix+".poison")) }()
	sibling, err := mAdmin.RegisterSchedule(ctx, prefix+".sibling", parse("@every 1m"), target.Name, payload, nil)
	must(err)
	defer func() { must(mAdmin.DestroySchedule(ctx, prefix+".sibling")) }()
	poisonedKey := poisoned.Name
	siblingKey := sibling.Name

	// registration validated the schedule, so corrupt the row directly --
	// every ClaimDueSchedule's ParseSchedule now fails for this row
	exec(ctx, `UPDATE schedule_config SET expression = 'not a expression' WHERE id = $1;`, poisoned.Id)
	backdate(ctx, poisoned.Id, time.Now().UTC().Add(-2*time.Hour))

	backdate(ctx, sibling.Id, time.Now().UTC().Add(-10*time.Second))
	waitAdvanced(ctx, sibling.Id)
	if got := messageCount(ctx, siblingKey); got != 1 {
		die(fmt.Sprintf("sibling should produce beside the poisoned row, got %d messages", got))
	}

	// the worker is still ticking: the sibling produces again while the
	// poisoned row keeps failing
	backdate(ctx, sibling.Id, time.Now().UTC().Add(-9*time.Second))
	waitAdvanced(ctx, sibling.Id)
	if got := messageCount(ctx, siblingKey); got != 2 {
		die(fmt.Sprintf("worker should keep ticking past the poisoned row, got %d sibling messages", got))
	}
	if got := messageCount(ctx, poisonedKey); got != 0 {
		die(fmt.Sprintf("poisoned row must not produce, got %d messages", got))
	}
	fmt.Println("  ✓ poisoned row produced nothing across 2 ticks, sibling produced both times")
}

func deferSection(ctx context.Context) {
	step("exclusive (spot): a scheduler request waits behind a running one, then runs")

	job, err := mAdmin.RegisterSchedule(ctx, prefix+".defer", parse("@every 1m"), target.Name, payload,
		&schedulecontroller.ScheduleConfig{Concurrency: common.ConcurrencyExclusive})
	must(err)
	defer func() { must(mAdmin.DestroySchedule(ctx, prefix+".defer")) }()

	groupName := prefix + ".defer.group"
	group := registerGroup(ctx, groupName, prefix+".defer")

	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	stop := startConsumer(ctx, groupName, []string{prefix + ".defer"}, 3, func(ctx context.Context, _ *labMessage) error {
		var first bool
		once.Do(func() { first = true })
		if first {
			close(started)
			<-release
		}
		return nil
	})
	defer stop()

	// both requests are scheduler-produced, so both carry the job's own
	// 'exclusive' -- the first is still running when the second lands (an 'parallel'
	// run never makes later requests wait, so run-now can't be the blocker)
	backdate(ctx, job.Id, time.Now().UTC().Add(-10*time.Second))
	waitAdvanced(ctx, job.Id)
	<-started

	backdate(ctx, job.Id, time.Now().UTC().Add(-9*time.Second))
	waitAdvanced(ctx, job.Id)
	deferred := scalarInt64(ctx, fmt.Sprintf(
		`SELECT MAX(id) FROM message_log_%d WHERE message_key = $1;`, target.Id),
		job.Name)

	// the 'deferred' row lands while the first request is still running
	waitForCount(ctx, fmt.Sprintf(
		`SELECT COUNT(*) FROM delivery_log_%d WHERE consumer_group_id = %d AND message_id = %d AND status = 'deferred';`,
		target.Id, group, deferred), 1)
	close(release)
	waitForCount(ctx, fmt.Sprintf(
		`SELECT COUNT(*) FROM delivery_log_%d WHERE consumer_group_id = %d AND message_id = %d AND status = 'success';`,
		target.Id, group, deferred), 1)
	fmt.Println("  ✓ scheduler request deferred behind the running one, then ran to success")
}

func runNowOverrideSection(ctx context.Context) {
	step("run-now beside a running request: default 'parallel' runs alongside it, cfg exclusive waits for it")

	job, err := mAdmin.RegisterSchedule(ctx, prefix+".runnow", parse("@hourly"), target.Name, payload,
		&schedulecontroller.ScheduleConfig{Concurrency: common.ConcurrencyExclusive})
	must(err)
	defer func() { must(mAdmin.DestroySchedule(ctx, prefix+".runnow")) }()

	groupName := prefix + ".runnow.group"
	group := registerGroup(ctx, groupName, prefix+".runnow")

	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	stop := startConsumer(ctx, groupName, []string{prefix + ".runnow"}, 3, func(ctx context.Context, _ *labMessage) error {
		var first bool
		once.Do(func() { first = true })
		if first {
			close(started)
			<-release
		}
		return nil
	})
	defer stop()

	// the blocker must be scheduler-produced: it carries the job's 'exclusive',
	// so later exclusive requests wait for it while the handler blocks
	backdate(ctx, job.Id, time.Now().UTC().Add(-2*time.Hour))
	waitAdvanced(ctx, job.Id)
	<-started
	blocker := scalarInt64(ctx, fmt.Sprintf(
		`SELECT MAX(id) FROM message_log_%d WHERE message_key = $1;`, target.Id),
		job.Name)

	// were the second request stamped with the job's 'exclusive', it would wait
	// until the first finishes -- the default 'parallel' runs it now
	override, err := mAdmin.RunSchedule(ctx, prefix+".runnow", nil)
	must(err)
	waitForCount(ctx, fmt.Sprintf(
		`SELECT COUNT(*) FROM delivery_log_%d WHERE consumer_group_id = %d AND message_id = %d AND status = 'success';`,
		target.Id, group, override.Id), 1)
	if got := scalarInt64(ctx, fmt.Sprintf(
		`SELECT COUNT(*) FROM delivery_log_%d WHERE consumer_group_id = %d AND message_id = %d AND status = 'success';`,
		target.Id, group, blocker)); got != 0 {
		die("the first request finished before the override ran -- the mid-run window was missed")
	}
	fmt.Println("  ✓ default run-now succeeded while the first was still running")

	// cfg.Concurrency exclusive opts back into the job's no-overlap safety: this
	// request waits for the running one instead of running beside it
	deferred, err := mAdmin.RunSchedule(ctx, prefix+".runnow", &admin.RunScheduleConfig{Concurrency: common.ConcurrencyExclusive})
	must(err)
	waitForCount(ctx, fmt.Sprintf(
		`SELECT COUNT(*) FROM delivery_log_%d WHERE consumer_group_id = %d AND message_id = %d AND status = 'deferred';`,
		target.Id, group, deferred.Id), 1)
	if got := scalarInt64(ctx, fmt.Sprintf(
		`SELECT COUNT(*) FROM delivery_log_%d WHERE consumer_group_id = %d AND message_id = %d AND status = 'success';`,
		target.Id, group, deferred.Id)); got != 0 {
		die("an exclusive run-now must not run while a previous request is still running")
	}
	close(release)
	waitForCount(ctx, fmt.Sprintf(
		`SELECT COUNT(*) FROM delivery_log_%d WHERE consumer_group_id = %d AND status = 'success';`,
		target.Id, group), 3)
	fmt.Println("  ✓ exclusive run-now waited for the running request, then ran")
}

func supersedeSection(ctx context.Context) {
	step("run-now supersedes a pending unclaimed request")

	job, err := mAdmin.RegisterSchedule(ctx, prefix+".supersede", parse("@hourly"), target.Name, payload, nil)
	must(err)
	defer func() { must(mAdmin.DestroySchedule(ctx, prefix+".supersede")) }()

	groupName := prefix + ".supersede.group"
	group := registerGroup(ctx, groupName, prefix+".supersede")

	// both requests land before any consumer claims -- the second takes over
	// the key's compaction_head pointer, and the claim query only returns a
	// keyed row while it IS the head, so the first is dropped unrun with no
	// delivery_log trace (the 'superseded' log row is the dispatched-then-
	// outraced exclusive path -- exclusivelab owns it)
	pending, err := mAdmin.RunSchedule(ctx, prefix+".supersede", nil)
	must(err)
	head, err := mAdmin.RunSchedule(ctx, prefix+".supersede", nil)
	must(err)

	if got := scalarInt64(ctx, fmt.Sprintf(`SELECT head_id FROM compaction_head_%d WHERE compaction_key = $1;`, target.Id),
		job.Name); got != head.Id {
		die(fmt.Sprintf("the second run-now must take the compaction head, got %d want %d", got, head.Id))
	}

	var handled int64
	var mu sync.Mutex
	stop := startConsumer(ctx, groupName, []string{prefix + ".supersede"}, 1, func(ctx context.Context, _ *labMessage) error {
		mu.Lock()
		defer mu.Unlock()
		handled++
		return nil
	})
	defer stop()

	waitForCount(ctx, fmt.Sprintf(
		`SELECT COUNT(*) FROM delivery_log_%d WHERE consumer_group_id = %d AND message_id = %d AND status = 'success';`,
		target.Id, group, head.Id), 1)
	if got := scalarInt64(ctx, fmt.Sprintf(
		`SELECT COUNT(*) FROM delivery_log_%d WHERE consumer_group_id = %d AND message_id = %d;`,
		target.Id, group, pending.Id)); got != 0 {
		die(fmt.Sprintf("the superseded request must leave no delivery rows, got %d", got))
	}
	mu.Lock()
	got := handled
	mu.Unlock()
	if got != 1 {
		die(fmt.Sprintf("the superseded request must never reach the handler, handled %d", got))
	}

	statuses, err := mAdmin.ScheduleStatus(ctx, prefix+".supersede")
	must(err)
	status := statusFor(statuses, groupName)
	if status.Ran != 1 || status.Succeeded != 1 || status.Failed != 0 {
		die(fmt.Sprintf("superseded requests must not count as ran: want 1/1/0, got %d/%d/%d", status.Ran, status.Succeeded, status.Failed))
	}
	if status.Superseded != 1 {
		die(fmt.Sprintf("the dropped request must count as superseded: want 1, got %d", status.Superseded))
	}

	// the request listing names the replacement: newest first, the dropped
	// request points at the one that replaced it
	requests, err := mAdmin.ScheduleMessages(ctx, prefix+".supersede", 20)
	must(err)
	if len(requests) != 2 {
		die(fmt.Sprintf("want 2 listed requests, got %d", len(requests)))
	}
	newest, oldest := requests[0], requests[1]
	if newest.MessageId != head.Id || newest.Outcome != schedule.MessageSucceeded {
		die(fmt.Sprintf("newest request: want %d succeeded, got %d %s", head.Id, newest.MessageId, newest.Outcome))
	}
	if oldest.MessageId != pending.Id || oldest.Outcome != schedule.MessageSuperseded {
		die(fmt.Sprintf("oldest request: want %d superseded, got %d %s", pending.Id, oldest.MessageId, oldest.Outcome))
	}
	if oldest.SupersededBy == nil || *oldest.SupersededBy != head.Id || oldest.SupersededAt == nil {
		die(fmt.Sprintf("the dropped request must name its replacement %d, got %+v", head.Id, oldest.SupersededBy))
	}
	fmt.Println("  ✓ first request superseded unrun, only the second ran; status counts 1/1/0 with superseded=1")
	fmt.Printf("  ✓ request listing: %d succeeded; %d superseded by %d at %s\n",
		newest.MessageId, oldest.MessageId, *oldest.SupersededBy, oldest.SupersededAt.Format("15:04:05"))
}

func statusSection(ctx context.Context) {
	step("consumer end-to-end + status: fail-once retry, always-failing sibling, RAN/SUCCEEDED/FAILED")

	jobName := prefix + ".status"
	_, err := mAdmin.RegisterSchedule(ctx, jobName, parse("@hourly"), target.Name, payload, nil)
	must(err)
	defer func() { must(mAdmin.DestroySchedule(ctx, jobName)) }()

	boundName := prefix + ".status.bound"
	otherName := prefix + ".status.other"
	bindinglessName := prefix + ".status.bindingless"
	bound := registerGroup(ctx, boundName, jobName)
	registerGroup(ctx, otherName, "some.other.job")
	registerGroup(ctx, bindinglessName)

	// first request: fail once then succeed (the retry); later requests fail
	// every attempt
	var mu sync.Mutex
	attempts := map[time.Time]int{}
	var firstScheduledTime time.Time
	stop := startConsumer(ctx, boundName, []string{jobName}, 1, func(ctx context.Context, _ *labMessage) error {
		meta, _ := consumergroup.MetaFromContext(ctx)
		mu.Lock()
		defer mu.Unlock()
		if firstScheduledTime.IsZero() {
			firstScheduledTime = meta.ScheduledAt
		}
		attempts[meta.ScheduledAt]++
		if meta.ScheduledAt.Equal(firstScheduledTime) && attempts[meta.ScheduledAt] > 1 {
			return nil
		}
		return errors.New("scripted failure")
	})
	defer stop()

	// wait out request 1's success before producing request 2, so the second
	// run-now can't supersede the first while it sits unclaimed
	first, err := mAdmin.RunSchedule(ctx, jobName, nil)
	must(err)
	waitForCount(ctx, fmt.Sprintf(
		`SELECT COUNT(*) FROM delivery_log_%d WHERE consumer_group_id = %d AND message_id = %d AND status = 'success';`,
		target.Id, bound, first.Id), 1)
	if got := scalarInt64(ctx, fmt.Sprintf(
		`SELECT COUNT(*) FROM delivery_log_%d WHERE consumer_group_id = %d AND message_id = %d AND status = 'failure';`,
		target.Id, bound, first.Id)); got < 1 {
		die("the first request must record its failed attempt before succeeding")
	}

	second, err := mAdmin.RunSchedule(ctx, jobName, nil)
	must(err)
	waitForCount(ctx, fmt.Sprintf(
		`SELECT COUNT(*) FROM delivery_log_%d WHERE consumer_group_id = %d AND message_id = %d AND status = 'failure';`,
		target.Id, bound, second.Id), 1)

	statuses, err := mAdmin.ScheduleStatus(ctx, jobName)
	must(err)
	for _, status := range statuses {
		fmt.Printf("  group=%s ran=%d succeeded=%d failed=%d superseded=%d\n", status.ConsumerGroup, status.Ran, status.Succeeded, status.Failed, status.Superseded)
	}
	if len(statuses) != 2 {
		die(fmt.Sprintf("want 2 matching groups (bound + bindingless), got %d", len(statuses)))
	}
	if statusFor(statuses, otherName) != nil {
		die("a group bound to a different name must not match")
	}
	boundStatus := statusFor(statuses, boundName)
	if boundStatus == nil || boundStatus.Ran != 2 || boundStatus.Succeeded != 1 || boundStatus.Failed != 1 {
		die(fmt.Sprintf("bound group: want ran=2 succeeded=1 failed=1, got %+v", boundStatus))
	}
	// the bound group RAN the first request before the second replaced it, so
	// nothing is superseded for it -- the bindingless group never ran it and
	// can never receive it now, so for that group it was dropped unrun
	if boundStatus.Superseded != 0 {
		die(fmt.Sprintf("bound group ran every request: want superseded=0, got %d", boundStatus.Superseded))
	}
	bindingless := statusFor(statuses, bindinglessName)
	if bindingless == nil || bindingless.Ran != 0 || bindingless.Succeeded != 0 || bindingless.Failed != 0 {
		die(fmt.Sprintf("bindingless group: want a 0/0/0 row, got %+v", bindingless))
	}
	if bindingless.Superseded != 1 {
		die(fmt.Sprintf("bindingless group: the replaced first request was dropped unrun for it, want superseded=1, got %d", bindingless.Superseded))
	}
	fmt.Println("  ✓ retried-then-succeeded counts once; bound 2/1/1 superseded=0, bindingless 0/0/0 superseded=1, non-matching absent")

	// per-group outcomes for the same two requests: the bound group ran both,
	// the bindingless group ran neither
	requests, err := mAdmin.ScheduleMessages(ctx, jobName, 20)
	must(err)
	outcomes := map[string]schedule.MessageOutcome{}
	for _, request := range requests {
		outcomes[fmt.Sprintf("%s/%d", request.ConsumerGroup, request.MessageId)] = request.Outcome
	}
	want := map[string]schedule.MessageOutcome{
		fmt.Sprintf("%s/%d", boundName, first.Id):        schedule.MessageSucceeded,
		fmt.Sprintf("%s/%d", boundName, second.Id):       schedule.MessageFailed,
		fmt.Sprintf("%s/%d", bindinglessName, first.Id):  schedule.MessageSuperseded,
		fmt.Sprintf("%s/%d", bindinglessName, second.Id): schedule.MessagePending,
	}
	if len(requests) != len(want) {
		die(fmt.Sprintf("want %d listed (request, group) rows, got %d", len(want), len(requests)))
	}
	for key, outcome := range want {
		if outcomes[key] != outcome {
			die(fmt.Sprintf("request %s: want %s, got %s", key, outcome, outcomes[key]))
		}
	}
	fmt.Println("  ✓ request listing per group: bound succeeded+failed, bindingless superseded+pending")
}

// --- harness ---

// startScheduler claims the system's schedule producer worker row with the lab's
// fast poll rate and runs it until the returned stop is called.
func startScheduler(ctx context.Context) func() {
	sys, err := mAdmin.GetSystem(ctx)
	must(err)
	owner, err := common.NewSystemOwner(sys.Id)
	must(err)
	workers, err := workercontroller.NewWorkerController(ds, nil)
	must(err)
	row, err := workers.GetWorker(ctx, scheduleproducer.WorkerScheduleProducer, owner)
	must(err)

	provisioner, err := scheduleproducer.NewScheduleProducerProvisioner(ds, nil)
	must(err)

	// a crashed earlier run's claim lingers until its InstanceTTL expires --
	// retry past it instead of dying
	row.Metadata = map[string]any{"poll_rate": int64(schedulerPollRate)}

	var execution worker.Execution
	deadline := time.Now().Add(60 * time.Second)
	for {
		execution, err = provisioner.Provision(ctx, row)
		must(err)
		if execution != nil {
			break
		}
		if time.Now().After(deadline) {
			die("schedule producer declined the instance for 60s -- is another claimant running?")
		}
		time.Sleep(time.Second)
	}

	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- execution.Run(runCtx) }()
	return func() {
		cancel()
		must(<-done)
	}
}

// registerGroup creates the consumer group on the lab's target topic, bound
// to the given schedule names (none = bindingless), and returns its id.
func registerGroup(ctx context.Context, name string, bindings ...string) int64 {
	controller, err := consumergroupcontroller.NewConsumerGroupController(ds, nil)
	must(err)
	group, err := controller.RegisterGroup(ctx, target.Id, name, consumergroup.Beginning())
	must(err)
	_, err = controller.DeclareBindings(ctx, target.Id, group.Id, bindings, time.Now())
	must(err)
	return group.Id
}

// startConsumer runs one consumer instance on the group until the returned
// stop is called.
func startConsumer(ctx context.Context, group string, bindings []string, concurrency int, handler func(context.Context, *labMessage) error) func() {
	labConsumer, err := consumer.NewConsumer(ds, &consumer.ConsumerConfig{
		ClaimPollRate:           schedulerPollRate,
		MessageConcurrency:      concurrency,
		ExceptionInitialBackoff: 200 * time.Millisecond,
	})
	must(err)

	lifecycleCtx, cancel := context.WithCancel(ctx)
	instance, err := labConsumer.Register[labMessage](lifecycleCtx, group, target.Name, bindings)
	must(err)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = instance.Consume(lifecycleCtx, handler)
	}()
	return func() {
		cancel()
		<-done
	}
}

func statusFor(statuses []*schedule.GroupStatus, group string) *schedule.GroupStatus {
	for _, status := range statuses {
		if status.ConsumerGroup == group {
			return status
		}
	}
	return nil
}

func cleanupTarget() {
	must(mAdmin.DestroyTopic(context.Background(), target.Name, admin.DestroyOptions{Force: true}))
}

// --- assertion helpers ---

func backdate(ctx context.Context, jobId int64, to time.Time) {
	exec(ctx, `UPDATE schedule_cursor SET next_scheduled_at = $1 WHERE schedule_id = $2;`, to, jobId)
}

// waitAdvanced returns once the scheduler has moved the row's
// next_scheduled_at back into the future -- its tick on the row is done.
func waitAdvanced(ctx context.Context, jobId int64) {
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		var advanced bool
		must(ds.Pool.QueryRow(ctx, `SELECT next_scheduled_at > now() FROM schedule_cursor WHERE schedule_id = $1;`, jobId).Scan(&advanced))
		if advanced {
			return
		}
		time.Sleep(schedulerPollRate / 2)
	}
	die(fmt.Sprintf("timed out waiting for schedule %d to advance", jobId))
}

func messageCount(ctx context.Context, messageKey string) int64 {
	return scalarInt64(ctx, fmt.Sprintf(
		`SELECT COUNT(*) FROM message_log_%d WHERE message_key = $1;`, target.Id), messageKey)
}

func producedScheduledTimes(ctx context.Context, messageKey string) []time.Time {
	rows, err := ds.Pool.Query(ctx, fmt.Sprintf(
		`SELECT options->>'scheduled_at' FROM message_log_%d WHERE message_key = $1 ORDER BY id;`, target.Id), messageKey)
	must(err)
	defer rows.Close()

	var times []time.Time
	for rows.Next() {
		var raw string
		must(rows.Scan(&raw))
		parsed, err := time.Parse(time.RFC3339Nano, raw)
		must(err)
		times = append(times, parsed)
	}
	must(rows.Err())
	return times
}

func waitForCount(ctx context.Context, sql string, want int64) {
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if scalarInt64(ctx, sql) >= want {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	die("timed out waiting for: " + sql)
}

func scalarInt64(ctx context.Context, sql string, args ...any) int64 {
	var value int64
	must(ds.Pool.QueryRow(ctx, sql, args...).Scan(&value))
	return value
}

func exec(ctx context.Context, sql string, args ...any) {
	_, err := ds.Pool.Exec(ctx, sql, args...)
	must(err)
}

func parse(expr string) *schedule.Expression {
	expression, err := schedule.ParseExpression(expr)
	must(err)
	return expression
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
