// Command cronlab proves the cron job machinery end to end against a live
// scheduler worker claimed with a fast poll rate.
//
// Sections:
//  1. validation -- charset/star names, sub-minute and no-upcoming schedules,
//     timeout vs min rate, re-register wins, Feb-29 single-scheduled-time pass
//  2. ownership -- a topic-owned job dies with its topic, a system-owned
//     job survives
//  3. produce-once -- a backdated row produces ONE message stamped with the
//     NEWEST due scheduled time, older dues dropped
//  4. v7 dedupe -- re-backdating to the SAME scheduled time is a Duplicate,
//     not a second message
//  5. suspend/unsuspend -- a suspended row never produces; a scheduled time
//     that came due while suspended is dropped, not produced late
//  6. poisoned row -- one job's produce fails every tick, siblings still
//     produce and the worker keeps ticking
//  7. defer (spot -- deferlab owns depth) -- a scheduler request lands while
//     a previous one is still running, waits, then runs
//  8. run-now beside a running request -- the default 'allow' runs alongside
//     it; cfg.Concurrency defer waits for it instead
//  9. run-now supersedes a pending unclaimed request
//  10. consumer end-to-end + status -- bind the job's name, fail-once retry,
//     'success' rows land, CronJobStatus shows RAN/SUCCEEDED/FAILED
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/agentstax/vulkan/pkg/admin"
	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/consumer"
	consumergroupcontroller "github.com/agentstax/vulkan/pkg/consumergroup/controller"
	"github.com/agentstax/vulkan/pkg/cron"
	croncontroller "github.com/agentstax/vulkan/pkg/cron/controller"
	"github.com/agentstax/vulkan/pkg/cron/scheduler"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/agentstax/vulkan/pkg/worker"
	workercontroller "github.com/agentstax/vulkan/pkg/worker/controller"
)

const schedulerPollRate = 100 * time.Millisecond

var (
	ds          *iDatastore.PostgresDatastore
	mAdmin      *admin.MessageAdmin
	jobRequests *topic.Topic
	prefix      string
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

	jobRequests, err = mAdmin.GetTopic(ctx, cron.TopicName, topic.SchemaVersion(1))
	must(err)

	prefix = fmt.Sprintf("cronlab.%d", time.Now().UnixNano())
	defer cleanupGroups()

	validationSection(ctx)
	ownershipSection(ctx)

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

	fmt.Println("\n✅ CRON LAB PASSED")
}

// --- sections ---

func validationSection(ctx context.Context) {
	step("validation: names, schedules, timeout vs min rate, re-register wins")

	hourly := parse("@hourly")

	if _, err := mAdmin.RegisterCronJob(ctx, "", hourly, nil, nil); err == nil {
		die("empty name must be rejected")
	}
	if _, err := mAdmin.RegisterCronJob(ctx, prefix+".Upper", hourly, nil, nil); err == nil {
		die("uppercase name must be rejected")
	}
	if _, err := mAdmin.RegisterCronJob(ctx, prefix+".star*", hourly, nil, nil); err == nil {
		die("'*' in a name is the binding wildcard and must be rejected")
	}
	if _, err := cron.ParseSchedule("@every 30s"); err == nil {
		die("sub-minute schedule must be rejected at parse")
	}
	// Feb 30 never exists, so the schedule has no upcoming scheduled time
	if _, err := cron.ParseSchedule("0 0 30 2 *"); err == nil {
		die("a schedule with no upcoming scheduled time must be rejected at parse")
	}
	if _, err := mAdmin.RegisterCronJob(ctx, prefix+".validate", hourly, nil, &croncontroller.CronJobConfig{Timeout: 2 * time.Hour}); err == nil {
		die("timeout above the schedule's min rate must be rejected")
	}
	fmt.Println("  ✓ rejections: empty/uppercase/star name, sub-minute, no-upcoming, timeout > min rate")

	// Feb-29 has under two scheduled times inside the min-rate horizon -- the
	// single-scheduled-time pass must register it, seeded on a real Feb 29
	feb29, err := mAdmin.RegisterCronJob(ctx, prefix+".feb29", parse("0 0 29 2 *"), nil, nil)
	must(err)
	if feb29.NextScheduledTime.UTC().Month() != time.February || feb29.NextScheduledTime.UTC().Day() != 29 {
		die(fmt.Sprintf("feb29 job seeded to %v, want a Feb 29", feb29.NextScheduledTime))
	}
	must(mAdmin.DestroyCronJob(ctx, prefix+".feb29"))
	fmt.Printf("  ✓ Feb-29 schedule registered, seeded to %s\n", feb29.NextScheduledTime.UTC().Format("2006-01-02"))

	first, err := mAdmin.RegisterCronJob(ctx, prefix+".redeclare", hourly, map[string]string{"kind": "lab"}, nil)
	must(err)
	again, err := mAdmin.RegisterCronJob(ctx, prefix+".redeclare", hourly, map[string]string{"kind": "lab"}, nil)
	must(err)
	if again.Id != first.Id {
		die(fmt.Sprintf("identical re-register resolved to a different job: %d vs %d", again.Id, first.Id))
	}

	daily := parse("@daily")
	must(mAdmin.SuspendCronJob(ctx, prefix+".redeclare"))
	redeclared, err := mAdmin.RegisterCronJob(ctx, prefix+".redeclare", daily, map[string]string{"kind": "lab"}, nil)
	must(err)
	if redeclared.Id != first.Id {
		die(fmt.Sprintf("re-register resolved to a different job: %d vs %d", redeclared.Id, first.Id))
	}
	if redeclared.Schedule != daily.String() {
		die(fmt.Sprintf("re-registered schedule = %q, want %q", redeclared.Schedule, daily))
	}
	if !redeclared.NextScheduledTime.After(time.Now().UTC()) {
		die(fmt.Sprintf("a schedule change must re-seed the next scheduled time, got %v", redeclared.NextScheduledTime))
	}
	if !redeclared.Suspended {
		die("a re-register must leave a suspended job suspended")
	}
	must(mAdmin.DestroyCronJob(ctx, prefix+".redeclare"))
	fmt.Println("  ✓ identical re-register is a no-op, a differing one wins and leaves suspended alone")
}

func ownershipSection(ctx context.Context) {
	step("ownership: topic-owned job cascades with its topic, system-owned survives")

	topicName := prefix + ".ownedtopic"
	tp, err := mAdmin.RegisterTopic(ctx, topicName, topic.SchemaVersion(1), nil)
	must(err)

	cronJobs, err := croncontroller.NewCronJobController(ds, nil)
	must(err)
	topicOwner, err := common.NewTopicOwner(tp.SystemId, tp.Id, tp.Name)
	must(err)
	_, err = cronJobs.Register(ctx, topicOwner, prefix+".cascade", parse("@hourly"), nil, nil)
	must(err)
	_, err = mAdmin.RegisterCronJob(ctx, prefix+".standalone", parse("@hourly"), nil, nil)
	must(err)

	must(mAdmin.DestroyTopic(ctx, topicName, topic.SchemaVersion(1), admin.DestroyOptions{Force: true}))

	cascaded, err := mAdmin.GetCronJob(ctx, prefix+".cascade")
	must(err)
	if cascaded != nil {
		die("topic-owned cron job must cascade away with its topic")
	}
	standalone, err := mAdmin.GetCronJob(ctx, prefix+".standalone")
	must(err)
	if standalone == nil {
		die("system-owned cron job must survive an unrelated topic destroy")
	}
	must(mAdmin.DestroyCronJob(ctx, prefix+".standalone"))
	fmt.Println("  ✓ cascade removed the topic-owned job, the system-owned job survived")
}

func produceOnceSection(ctx context.Context) {
	step("produce-once: a 5m-backdated row produces ONE message, stamped with the NEWEST due scheduled time")

	job, err := mAdmin.RegisterCronJob(ctx, prefix+".walk", parse("@every 1m"), nil, nil)
	must(err)
	defer func() { must(mAdmin.DestroyCronJob(ctx, prefix+".walk")) }()
	key := strconv.FormatInt(job.Id, 10)

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

	job, err := mAdmin.RegisterCronJob(ctx, prefix+".dedupe", parse("@every 1m"), nil, nil)
	must(err)
	defer func() { must(mAdmin.DestroyCronJob(ctx, prefix+".dedupe")) }()
	key := strconv.FormatInt(job.Id, 10)

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

	job, err := mAdmin.RegisterCronJob(ctx, prefix+".suspend", parse("@hourly"), nil, nil)
	must(err)
	defer func() { must(mAdmin.DestroyCronJob(ctx, prefix+".suspend")) }()
	key := strconv.FormatInt(job.Id, 10)

	must(mAdmin.SuspendCronJob(ctx, prefix+".suspend"))
	backdate(ctx, job.Id, time.Now().UTC().Add(-2*time.Hour))
	time.Sleep(10 * schedulerPollRate)
	if got := messageCount(ctx, key); got != 0 {
		die(fmt.Sprintf("suspended row produced %d messages", got))
	}

	must(mAdmin.UnsuspendCronJob(ctx, prefix+".suspend"))
	unsuspended, err := mAdmin.GetCronJob(ctx, prefix+".suspend")
	must(err)
	if !unsuspended.NextScheduledTime.After(time.Now()) {
		die(fmt.Sprintf("unsuspend must re-seed next_scheduled_time in the future, got %v", unsuspended.NextScheduledTime))
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

	poisoned, err := mAdmin.RegisterCronJob(ctx, prefix+".poison", parse("@hourly"), nil, nil)
	must(err)
	defer func() { must(mAdmin.DestroyCronJob(ctx, prefix+".poison")) }()
	sibling, err := mAdmin.RegisterCronJob(ctx, prefix+".sibling", parse("@every 1m"), nil, nil)
	must(err)
	defer func() { must(mAdmin.DestroyCronJob(ctx, prefix+".sibling")) }()
	poisonedKey := strconv.FormatInt(poisoned.Id, 10)
	siblingKey := strconv.FormatInt(sibling.Id, 10)

	// registration validated the schedule, so corrupt the row directly --
	// every ClaimDueCronJob's ParseSchedule now fails for this row
	exec(ctx, `UPDATE cron_job SET schedule = 'not a schedule' WHERE id = $1;`, poisoned.Id)
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
	step("defer (spot): a scheduler request waits behind a running one, then runs")

	job, err := mAdmin.RegisterCronJob(ctx, prefix+".defer", parse("@every 1m"), nil,
		&croncontroller.CronJobConfig{Concurrency: common.ConcurrencyDefer})
	must(err)
	defer func() { must(mAdmin.DestroyCronJob(ctx, prefix+".defer")) }()

	groupName := prefix + ".defer.group"
	group := registerGroup(ctx, groupName, prefix+".defer")

	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	stop := startConsumer(ctx, groupName, []string{prefix + ".defer"}, 3, func(ctx context.Context, request *cron.JobRequest) error {
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
	// 'defer' -- the first is still running when the second lands (an 'allow'
	// run never makes later requests wait, so run-now can't be the blocker)
	backdate(ctx, job.Id, time.Now().UTC().Add(-10*time.Second))
	waitAdvanced(ctx, job.Id)
	<-started

	backdate(ctx, job.Id, time.Now().UTC().Add(-9*time.Second))
	waitAdvanced(ctx, job.Id)
	deferred := scalarInt64(ctx, fmt.Sprintf(
		`SELECT MAX(id) FROM message_log_%d WHERE compaction_key = $1;`, jobRequests.Id),
		strconv.FormatInt(job.Id, 10))

	// the 'deferred' row lands while the first request is still running
	waitForCount(ctx, fmt.Sprintf(
		`SELECT COUNT(*) FROM delivery_log_%d WHERE consumer_group_id = %d AND message_id = %d AND status = 'deferred';`,
		jobRequests.Id, group, deferred), 1)
	close(release)
	waitForCount(ctx, fmt.Sprintf(
		`SELECT COUNT(*) FROM delivery_log_%d WHERE consumer_group_id = %d AND message_id = %d AND status = 'success';`,
		jobRequests.Id, group, deferred), 1)
	fmt.Println("  ✓ scheduler request deferred behind the running one, then ran to success")
}

func runNowOverrideSection(ctx context.Context) {
	step("run-now beside a running request: default 'allow' runs alongside it, cfg defer waits for it")

	job, err := mAdmin.RegisterCronJob(ctx, prefix+".runnow", parse("@hourly"), nil,
		&croncontroller.CronJobConfig{Concurrency: common.ConcurrencyDefer})
	must(err)
	defer func() { must(mAdmin.DestroyCronJob(ctx, prefix+".runnow")) }()

	groupName := prefix + ".runnow.group"
	group := registerGroup(ctx, groupName, prefix+".runnow")

	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	stop := startConsumer(ctx, groupName, []string{prefix + ".runnow"}, 3, func(ctx context.Context, request *cron.JobRequest) error {
		var first bool
		once.Do(func() { first = true })
		if first {
			close(started)
			<-release
		}
		return nil
	})
	defer stop()

	// the blocker must be scheduler-produced: it carries the job's 'defer',
	// so later defer requests wait for it while the handler blocks
	backdate(ctx, job.Id, time.Now().UTC().Add(-2*time.Hour))
	waitAdvanced(ctx, job.Id)
	<-started
	blocker := scalarInt64(ctx, fmt.Sprintf(
		`SELECT MAX(id) FROM message_log_%d WHERE compaction_key = $1;`, jobRequests.Id),
		strconv.FormatInt(job.Id, 10))

	// were the second request stamped with the job's 'defer', it would wait
	// until the first finishes -- the default 'allow' runs it now
	override, err := mAdmin.RunCronJob(ctx, prefix+".runnow", nil)
	must(err)
	waitForCount(ctx, fmt.Sprintf(
		`SELECT COUNT(*) FROM delivery_log_%d WHERE consumer_group_id = %d AND message_id = %d AND status = 'success';`,
		jobRequests.Id, group, override.Id), 1)
	if got := scalarInt64(ctx, fmt.Sprintf(
		`SELECT COUNT(*) FROM delivery_log_%d WHERE consumer_group_id = %d AND message_id = %d AND status = 'success';`,
		jobRequests.Id, group, blocker)); got != 0 {
		die("the first request finished before the override ran -- the mid-run window was missed")
	}
	fmt.Println("  ✓ default run-now succeeded while the first was still running")

	// cfg.Concurrency defer opts back into the job's no-overlap safety: this
	// request waits for the running one instead of running beside it
	deferred, err := mAdmin.RunCronJob(ctx, prefix+".runnow", &admin.RunCronJobConfig{Concurrency: common.ConcurrencyDefer})
	must(err)
	waitForCount(ctx, fmt.Sprintf(
		`SELECT COUNT(*) FROM delivery_log_%d WHERE consumer_group_id = %d AND message_id = %d AND status = 'deferred';`,
		jobRequests.Id, group, deferred.Id), 1)
	if got := scalarInt64(ctx, fmt.Sprintf(
		`SELECT COUNT(*) FROM delivery_log_%d WHERE consumer_group_id = %d AND message_id = %d AND status = 'success';`,
		jobRequests.Id, group, deferred.Id)); got != 0 {
		die("a defer run-now must not run while a previous request is still running")
	}
	close(release)
	waitForCount(ctx, fmt.Sprintf(
		`SELECT COUNT(*) FROM delivery_log_%d WHERE consumer_group_id = %d AND status = 'success';`,
		jobRequests.Id, group), 3)
	fmt.Println("  ✓ defer run-now waited for the running request, then ran")
}

func supersedeSection(ctx context.Context) {
	step("run-now supersedes a pending unclaimed request")

	job, err := mAdmin.RegisterCronJob(ctx, prefix+".supersede", parse("@hourly"), nil, nil)
	must(err)
	defer func() { must(mAdmin.DestroyCronJob(ctx, prefix+".supersede")) }()

	groupName := prefix + ".supersede.group"
	group := registerGroup(ctx, groupName, prefix+".supersede")

	// both requests land before any consumer claims -- the second takes over
	// the key's compaction_head pointer, and the claim query only returns a
	// keyed row while it IS the head, so the first is dropped unrun with no
	// delivery_log trace (the 'superseded' log row is the dispatched-then-
	// outraced defer path -- deferlab owns it)
	pending, err := mAdmin.RunCronJob(ctx, prefix+".supersede", nil)
	must(err)
	head, err := mAdmin.RunCronJob(ctx, prefix+".supersede", nil)
	must(err)

	if got := scalarInt64(ctx, fmt.Sprintf(`SELECT head_id FROM compaction_head_%d WHERE compaction_key = $1;`, jobRequests.Id),
		strconv.FormatInt(job.Id, 10)); got != head.Id {
		die(fmt.Sprintf("the second run-now must take the compaction head, got %d want %d", got, head.Id))
	}

	var handled int64
	var mu sync.Mutex
	stop := startConsumer(ctx, groupName, []string{prefix + ".supersede"}, 1, func(ctx context.Context, request *cron.JobRequest) error {
		mu.Lock()
		defer mu.Unlock()
		handled++
		return nil
	})
	defer stop()

	waitForCount(ctx, fmt.Sprintf(
		`SELECT COUNT(*) FROM delivery_log_%d WHERE consumer_group_id = %d AND message_id = %d AND status = 'success';`,
		jobRequests.Id, group, head.Id), 1)
	if got := scalarInt64(ctx, fmt.Sprintf(
		`SELECT COUNT(*) FROM delivery_log_%d WHERE consumer_group_id = %d AND message_id = %d;`,
		jobRequests.Id, group, pending.Id)); got != 0 {
		die(fmt.Sprintf("the superseded request must leave no delivery rows, got %d", got))
	}
	mu.Lock()
	got := handled
	mu.Unlock()
	if got != 1 {
		die(fmt.Sprintf("the superseded request must never reach the handler, handled %d", got))
	}

	statuses, err := mAdmin.CronJobStatus(ctx, prefix+".supersede")
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
	requests, err := mAdmin.CronJobRequests(ctx, prefix+".supersede", 20)
	must(err)
	if len(requests) != 2 {
		die(fmt.Sprintf("want 2 listed requests, got %d", len(requests)))
	}
	newest, oldest := requests[0], requests[1]
	if newest.MessageId != head.Id || newest.Outcome != cron.JobRequestSucceeded {
		die(fmt.Sprintf("newest request: want %d succeeded, got %d %s", head.Id, newest.MessageId, newest.Outcome))
	}
	if oldest.MessageId != pending.Id || oldest.Outcome != cron.JobRequestSuperseded {
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
	_, err := mAdmin.RegisterCronJob(ctx, jobName, parse("@hourly"), nil, nil)
	must(err)
	defer func() { must(mAdmin.DestroyCronJob(ctx, jobName)) }()

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
	stop := startConsumer(ctx, boundName, []string{jobName}, 1, func(ctx context.Context, request *cron.JobRequest) error {
		mu.Lock()
		defer mu.Unlock()
		if firstScheduledTime.IsZero() {
			firstScheduledTime = request.ScheduledTime
		}
		attempts[request.ScheduledTime]++
		if request.ScheduledTime.Equal(firstScheduledTime) && attempts[request.ScheduledTime] > 1 {
			return nil
		}
		return errors.New("scripted failure")
	})
	defer stop()

	// wait out request 1's success before producing request 2, so the second
	// run-now can't supersede the first while it sits unclaimed
	first, err := mAdmin.RunCronJob(ctx, jobName, nil)
	must(err)
	waitForCount(ctx, fmt.Sprintf(
		`SELECT COUNT(*) FROM delivery_log_%d WHERE consumer_group_id = %d AND message_id = %d AND status = 'success';`,
		jobRequests.Id, bound, first.Id), 1)
	if got := scalarInt64(ctx, fmt.Sprintf(
		`SELECT COUNT(*) FROM delivery_log_%d WHERE consumer_group_id = %d AND message_id = %d AND status = 'failure';`,
		jobRequests.Id, bound, first.Id)); got < 1 {
		die("the first request must record its failed attempt before succeeding")
	}

	second, err := mAdmin.RunCronJob(ctx, jobName, nil)
	must(err)
	waitForCount(ctx, fmt.Sprintf(
		`SELECT COUNT(*) FROM delivery_log_%d WHERE consumer_group_id = %d AND message_id = %d AND status = 'failure';`,
		jobRequests.Id, bound, second.Id), 1)

	statuses, err := mAdmin.CronJobStatus(ctx, jobName)
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
	requests, err := mAdmin.CronJobRequests(ctx, jobName, 20)
	must(err)
	outcomes := map[string]cron.JobRequestOutcome{}
	for _, request := range requests {
		outcomes[fmt.Sprintf("%s/%d", request.ConsumerGroup, request.MessageId)] = request.Outcome
	}
	want := map[string]cron.JobRequestOutcome{
		fmt.Sprintf("%s/%d", boundName, first.Id):        cron.JobRequestSucceeded,
		fmt.Sprintf("%s/%d", boundName, second.Id):       cron.JobRequestFailed,
		fmt.Sprintf("%s/%d", bindinglessName, first.Id):  cron.JobRequestSuperseded,
		fmt.Sprintf("%s/%d", bindinglessName, second.Id): cron.JobRequestPending,
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

// startScheduler claims the system's cron scheduler worker row with the lab's
// fast poll rate and runs it until the returned stop is called.
func startScheduler(ctx context.Context) func() {
	sys, err := mAdmin.GetSystem(ctx)
	must(err)
	owner, err := common.NewSystemOwner(sys.Id)
	must(err)
	workers, err := workercontroller.NewWorkerController(ds, nil)
	must(err)
	row, err := workers.GetWorker(ctx, scheduler.WorkerCronScheduler, owner)
	must(err)

	provisioner, err := scheduler.NewCronSchedulerProvisioner(ds, nil)
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
			die("cron scheduler declined the instance for 60s -- is another claimant running?")
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

// registerGroup creates the consumer group on the job_requests topic, bound to
// the given job names (none = bindingless), and returns its id.
func registerGroup(ctx context.Context, name string, bindings ...string) int64 {
	controller, err := consumergroupcontroller.NewConsumerGroupController(ds, nil)
	must(err)
	group, err := controller.RegisterGroup(ctx, jobRequests.Id, name)
	must(err)
	_, err = controller.DeclareBindings(ctx, jobRequests.Id, group.Id, bindings, time.Now())
	must(err)
	return group.Id
}

// startConsumer runs one consumer instance on the group until the returned
// stop is called.
func startConsumer(ctx context.Context, group string, bindings []string, concurrency int, handler func(context.Context, *cron.JobRequest) error) func() {
	jobRequestConsumer, err := consumer.NewConsumer[cron.JobRequest](ds, &consumer.ConsumerConfig{
		ClaimPollRate:           schedulerPollRate,
		MessageConcurrency:      concurrency,
		ExceptionInitialBackoff: 200 * time.Millisecond,
	})
	must(err)

	lifecycleCtx, cancel := context.WithCancel(ctx)
	instance, err := jobRequestConsumer.Register(lifecycleCtx, group, cron.TopicName, topic.SchemaVersion(1), bindings)
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

func statusFor(statuses []*cron.GroupStatus, group string) *cron.GroupStatus {
	for _, status := range statuses {
		if status.ConsumerGroup == group {
			return status
		}
	}
	return nil
}

func cleanupGroups() {
	ctx := context.Background()
	for _, sql := range []string{
		fmt.Sprintf(`DELETE FROM exception_queue_%d WHERE consumer_group_id IN (SELECT id FROM consumer_group_config WHERE name LIKE '%s.%%');`, jobRequests.Id, prefix),
		fmt.Sprintf(`DELETE FROM delivery_log_%d WHERE consumer_group_id IN (SELECT id FROM consumer_group_config WHERE name LIKE '%s.%%');`, jobRequests.Id, prefix),
		fmt.Sprintf(`DELETE FROM claim_lease_%d WHERE consumer_group_id IN (SELECT id FROM consumer_group_config WHERE name LIKE '%s.%%');`, jobRequests.Id, prefix),
		fmt.Sprintf(`DELETE FROM consumer_group_config WHERE name LIKE '%s.%%';`, prefix),
	} {
		exec(ctx, sql)
	}
}

// --- assertion helpers ---

func backdate(ctx context.Context, jobId int64, to time.Time) {
	exec(ctx, `UPDATE cron_job SET next_scheduled_time = $1 WHERE id = $2;`, to, jobId)
}

// waitAdvanced returns once the scheduler has moved the row's
// next_scheduled_time back into the future -- its tick on the row is done.
func waitAdvanced(ctx context.Context, jobId int64) {
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		var advanced bool
		must(ds.Pool.QueryRow(ctx, `SELECT next_scheduled_time > now() FROM cron_job WHERE id = $1;`, jobId).Scan(&advanced))
		if advanced {
			return
		}
		time.Sleep(schedulerPollRate / 2)
	}
	die(fmt.Sprintf("timed out waiting for cron_job %d to advance", jobId))
}

func messageCount(ctx context.Context, compactionKey string) int64 {
	return scalarInt64(ctx, fmt.Sprintf(
		`SELECT COUNT(*) FROM message_log_%d WHERE compaction_key = $1;`, jobRequests.Id), compactionKey)
}

func producedScheduledTimes(ctx context.Context, compactionKey string) []time.Time {
	rows, err := ds.Pool.Query(ctx, fmt.Sprintf(
		`SELECT payload->>'scheduled_time' FROM message_log_%d WHERE compaction_key = $1 ORDER BY id;`, jobRequests.Id), compactionKey)
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

func parse(expr string) *cron.Schedule {
	schedule, err := cron.ParseSchedule(expr)
	must(err)
	return schedule
}

func step(s string) { fmt.Printf("\n--- %s ---\n", s) }

func must(err error) {
	if err != nil {
		die(err.Error())
	}
}

func die(msg string) {
	fmt.Println("❌ " + msg)
	os.Exit(1)
}
