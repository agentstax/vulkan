// Command alertlab proves the default-alert machinery end to end: what
// RegisterSystem seeds, every classify arm, and the live partition_count
// executor claimed as a real worker.
//
// Sections:
//  1. seeding -- both alert cron jobs + consumer groups + exact declarations + worker
//     rows exist after RegisterSystem; a declared threshold applies on a
//     re-register and a suspended job survives one
//  2. classify -- driven through AlertController with a 2s repeat: the active
//     edge WARNs once, an unchanged condition publishes nothing, the repeat
//     republish moves the head to a fresh row, a severity change publishes
//     silently, the resolve edge INFOs once, resolved stays silent
//  3. executor -- the real partition_count worker: a threshold-1 run
//     publishes active heads + WARN edges, a second run inside the repeat
//     interval publishes nothing, foreign and bindingless groups on the
//     job_requests topic receive nothing
//  4. isolation -- one owner's corrupted head fails its Record while every
//     other topic still resolves; fixing the head lets the retry resolve it
package main

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/agentstax/vulkan/pkg/admin"
	"github.com/agentstax/vulkan/pkg/alert"
	"github.com/agentstax/vulkan/pkg/alert/compactionreadcost"
	alertcontroller "github.com/agentstax/vulkan/pkg/alert/controller"
	"github.com/agentstax/vulkan/pkg/alert/partitioncount"
	partitioncountcontroller "github.com/agentstax/vulkan/pkg/alert/partitioncount/controller"
	"github.com/agentstax/vulkan/pkg/common"
	compactioncontroller "github.com/agentstax/vulkan/pkg/compaction/controller"
	"github.com/agentstax/vulkan/pkg/consumergroup"
	consumergroupcontroller "github.com/agentstax/vulkan/pkg/consumergroup/controller"
	"github.com/agentstax/vulkan/pkg/cron"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	iMetrics "github.com/agentstax/vulkan/pkg/metrics"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/agentstax/vulkan/pkg/worker"
	workercontroller "github.com/agentstax/vulkan/pkg/worker/controller"
)

const (
	labCheckName   = "labcheck"
	classifyRepeat = 2 * time.Second
)

// labMessage is the lab topic's payload -- its one write creates the
// topic's first partition, so threshold 1 can trip on it.
type labMessage struct {
	Value string
}

func (labMessage) SchemaVersion() topic.SchemaVersion { return 1 }

var (
	ds     *iDatastore.PostgresDatastore
	mAdmin *admin.MessageAdmin

	jobRequests *topic.Topic
	alertsTopic *topic.Topic
	prefix      string

	partitionCountGroup int64
	groupOwner          *common.Owner
	labTopic            *topic.Topic
	labTopicOwner       *common.Owner
	executorCapture     *captureLogger
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

	mAdmin, err = admin.NewMessageAdmin(ds, &admin.MessageAdminConfig{AllowDestroy: true})
	must(err)
	must(mAdmin.RegisterSystem(ctx, nil))

	jobRequests, err = mAdmin.GetTopic(ctx, cron.TopicName)
	must(err)
	alertsTopic, err = mAdmin.GetTopic(ctx, alert.TopicName)
	must(err)
	if jobRequests == nil || alertsTopic == nil {
		die("RegisterSystem must create the job_requests and alerts topics")
	}

	prefix = fmt.Sprintf("alertlab.%d", time.Now().UnixNano())
	defer cleanup()

	seedingSection(ctx)
	classifySection(ctx)

	// the executor's embedded consumer spawns a cron scheduler -- suspend the
	// alert cron jobs so only the lab's run-nows produce requests (a suspended
	// job still runs on run-now)
	must(mAdmin.SuspendCronJob(ctx, partitioncount.JobName))
	must(mAdmin.SuspendCronJob(ctx, compactionreadcost.JobName))

	executorCapture = newCaptureLogger()
	stopExecutor := startExecutor(ctx)
	defer stopExecutor()

	executorSection(ctx)
	isolationSection(ctx)

	fmt.Println("\n✅ ALERT LAB PASSED")
	return nil
}

// --- sections ---

func seedingSection(ctx context.Context) {
	step("seeding: jobs/groups/bindings/workers exist; declared threshold applies, suspended survives")

	partitionCountJob, err := mAdmin.GetCronJob(ctx, partitioncount.JobName)
	must(err)
	if partitionCountJob == nil {
		die("RegisterSystem must seed the " + partitioncount.JobName + " cron job")
	}
	seeded, err := alertcontroller.ToJobPayload(partitionCountJob.Payload)
	must(err)
	if seeded.Threshold != 0 || partitionCountJob.Concurrency != common.ConcurrencyExclusive {
		die(fmt.Sprintf("seeded job: want threshold 0 + defer, got %d %s", seeded.Threshold, partitionCountJob.Concurrency))
	}
	readCostJob, err := mAdmin.GetCronJob(ctx, compactionreadcost.JobName)
	must(err)
	if readCostJob == nil {
		die("RegisterSystem must seed the " + compactionreadcost.JobName + " cron job")
	}

	declarations, err := mAdmin.ListDeclarations(ctx)
	must(err)
	for _, jobName := range []string{partitioncount.JobName, compactionreadcost.JobName} {
		declared := false
		for _, declaration := range declarations {
			if declaration.GroupName == jobName && declaration.TopicName == cron.TopicName &&
				declaration.Status == consumergroup.DeclarationInstalled &&
				len(declaration.Patterns) == 1 && declaration.Patterns[0] == jobName {
				declared = true
			}
		}
		if !declared {
			die("group " + jobName + " must declare exactly its job name at RegisterSystem")
		}
	}

	partitionCountGroup = scalarInt64(ctx,
		`SELECT id FROM consumer_group_config WHERE topic_id = $1 AND name = $2;`,
		jobRequests.Id, partitioncount.JobName)
	groupOwner, err = common.NewConsumerGroupOwner(jobRequests.SystemId, jobRequests.Id, partitionCountGroup, partitioncount.JobName)
	must(err)
	workers, err := workercontroller.NewWorkerController(ds, nil)
	must(err)
	row, err := workers.GetWorker(ctx, partitioncount.JobName, groupOwner)
	must(err)
	if row == nil {
		die("RegisterSystem must declare the " + partitioncount.JobName + " worker row")
	}
	fmt.Println("  ✓ both alert cron jobs, exact declarations, and the worker row exist")

	// a declared threshold applies on every RegisterSystem, and a suspended
	// alert job stays suspended through one
	must(mAdmin.SuspendCronJob(ctx, compactionreadcost.JobName))
	declareThreshold(ctx, 7)

	reread, err := mAdmin.GetCronJob(ctx, partitioncount.JobName)
	must(err)
	redeclared, err := alertcontroller.ToJobPayload(reread.Payload)
	must(err)
	if redeclared.Threshold != 7 {
		die(fmt.Sprintf("declared threshold must apply on re-register, got %d", redeclared.Threshold))
	}
	readCostJob, err = mAdmin.GetCronJob(ctx, compactionreadcost.JobName)
	must(err)
	if !readCostJob.Suspended {
		die("a suspended alert cron job must survive re-register")
	}

	declareThreshold(ctx, 0)
	must(mAdmin.UnsuspendCronJob(ctx, compactionreadcost.JobName))
	fmt.Println("  ✓ declared threshold applied, suspended state survived re-register")
}

// declareThreshold re-declares the partition_count alert at threshold, through
// the same call a user changing it would make.
func declareThreshold(ctx context.Context, threshold int64) {
	must(mAdmin.RegisterSystem(ctx, &admin.RegisterSystemConfig{
		PartitionCount: &partitioncount.JobConfig{Threshold: threshold},
	}))
}

func classifySection(ctx context.Context) {
	step("classify: edge WARN, quiet hold, repeat republish, silent severity change, resolve INFO")

	var err error
	labTopic, err = mAdmin.RegisterTopic(ctx, prefix+".topic", nil)
	must(err)
	labTopicOwner, err = common.NewTopicOwner(labTopic.SystemId, labTopic.Id, labTopic.Name)
	must(err)

	alertProducer, err := producer.NewProducer[alert.Alert](ds, nil)
	must(err)
	instance, err := alertProducer.Register(ctx, alert.TopicName)
	must(err)
	heads, err := compactioncontroller.NewCompactionController[alert.Alert](ds, nil)
	must(err)
	capture := newCaptureLogger()
	alerts, err := alertcontroller.NewAlertController(ctx, instance, heads, classifyRepeat, &alertcontroller.ControllerConfig{Logger: capture})
	must(err)

	key, err := alert.MessageKey(labCheckName, labTopicOwner)
	must(err)
	found, err := alert.NewAlert(labCheckName, labTopicOwner, alert.StatusActive, alert.SeverityWarn, "labcheck condition holds", nil)
	must(err)

	record := func(found *alert.Alert, want alert.RecordOutcome, arm string) {
		outcome, err := alerts.Record(ctx, labCheckName, labTopicOwner, found)
		must(err)
		if outcome != want {
			die(fmt.Sprintf("%s: want outcome %q, got %q", arm, want, outcome))
		}
	}

	// active edge: first publish moves the head and WARNs once
	record(found, alert.RecordOutcomeActive, "active edge")
	if got := alertMessageCount(ctx, key); got != 1 {
		die(fmt.Sprintf("active edge: want 1 published message, got %d", got))
	}
	if got := headStatus(ctx, key); got != string(alert.StatusActive) {
		die(fmt.Sprintf("active edge: want head status active, got %q", got))
	}
	if got := capture.count("warn", labCheckName, labTopic.Name); got != 1 {
		die(fmt.Sprintf("active edge: want 1 WARN, got %d", got))
	}
	fmt.Println("  ✓ active edge published the head and WARNed once")

	// unchanged condition inside the repeat interval: nothing publishes
	record(found, alert.RecordOutcomeNothing, "quiet hold")
	if got := alertMessageCount(ctx, key); got != 1 {
		die(fmt.Sprintf("quiet hold: want no republish inside the repeat interval, got %d messages", got))
	}
	fmt.Println("  ✓ unchanged condition inside the interval published nothing")

	// repeat republish: the same alert past the interval republishes
	// silently, moving the head to a fresh row so retention can't sweep a
	// live alert
	firstHead := headId(ctx, key)
	time.Sleep(classifyRepeat + 500*time.Millisecond)
	record(found, alert.RecordOutcomeActive, "repeat")
	if got := alertMessageCount(ctx, key); got != 2 {
		die(fmt.Sprintf("repeat: want a republish past the interval, got %d messages", got))
	}
	if got := headId(ctx, key); got == firstHead {
		die("repeat: the republish must move the head to the fresh row")
	}
	if got := capture.count("warn", labCheckName, labTopic.Name); got != 1 {
		die(fmt.Sprintf("repeat: the republish must be silent, got %d WARNs", got))
	}
	fmt.Println("  ✓ repeat republish refreshed the head silently")

	// severity change: publishes immediately (still inside the interval),
	// silently -- the head's stored severity is doctored by direct SQL,
	// bypassing the controller
	exec(ctx, fmt.Sprintf(
		`UPDATE message_log_%d SET payload = jsonb_set(payload, '{severity}', '"lab-critical"') WHERE id = $1;`,
		alertsTopic.Id), headId(ctx, key))
	record(found, alert.RecordOutcomeActive, "severity change")
	if got := alertMessageCount(ctx, key); got != 3 {
		die(fmt.Sprintf("severity change: want an immediate republish, got %d messages", got))
	}
	if got := capture.count("warn", labCheckName, labTopic.Name); got != 1 {
		die(fmt.Sprintf("severity change: the republish must be silent, got %d WARNs", got))
	}
	fmt.Println("  ✓ severity change republished immediately and silently")

	// resolve edge: a nil finding resolves the head with one INFO
	record(nil, alert.RecordOutcomeResolved, "resolve edge")
	if got := alertMessageCount(ctx, key); got != 4 {
		die(fmt.Sprintf("resolve edge: want a resolve publish, got %d messages", got))
	}
	if got := headStatus(ctx, key); got != string(alert.StatusResolved) {
		die(fmt.Sprintf("resolve edge: want head status resolved, got %q", got))
	}
	if got := capture.count("info", labCheckName, labTopic.Name); got != 1 {
		die(fmt.Sprintf("resolve edge: want 1 INFO, got %d", got))
	}

	// resolved head + nil finding: nothing
	record(nil, alert.RecordOutcomeNothing, "resolved + nothing found")
	if got := alertMessageCount(ctx, key); got != 4 {
		die(fmt.Sprintf("resolved + nothing found must publish nothing, got %d messages", got))
	}
	fmt.Println("  ✓ resolve edge INFOed once, resolved head stayed silent")
}

func executorSection(ctx context.Context) {
	step("executor: threshold-1 run alerts, repeat-interval run is quiet, foreign groups untouched")

	// one write gives the lab topic its first partition
	labProducer, err := producer.NewProducer[labMessage](ds, nil)
	must(err)
	labInstance, err := labProducer.Register(ctx, labTopic.Name)
	must(err)
	_, err = labInstance.Produce(ctx, &labMessage{Value: "seed"}, producer.ProduceOptions{})
	must(err)

	otherGroup := registerGroup(ctx, prefix+".other", "some.other.job")
	bindinglessGroup := registerGroup(ctx, prefix+".bindingless")

	declareThreshold(ctx, 1)

	firstRun, err := mAdmin.RunCronJob(ctx, partitioncount.JobName, nil)
	must(err)
	waitDelivered(ctx, firstRun.Id, "success")

	// the running executor's Register declared the group's set
	declarations, err := mAdmin.ListDeclarations(ctx)
	must(err)
	declared := false
	for _, declaration := range declarations {
		if declaration.GroupName == partitioncount.JobName && declaration.TopicName == cron.TopicName &&
			declaration.Status == consumergroup.DeclarationInstalled &&
			len(declaration.Patterns) == 1 && declaration.Patterns[0] == partitioncount.JobName {
			declared = true
		}
	}
	if !declared {
		die("the live executor must declare exactly its job name")
	}
	fmt.Println("  ✓ the executor declared exactly its job name")

	labKey := partitionCountKey(labTopicOwner)
	jobRequestsOwner, err := common.NewTopicOwner(jobRequests.SystemId, jobRequests.Id, jobRequests.Name)
	must(err)
	jobRequestsKey := partitionCountKey(jobRequestsOwner)
	if got := headStatus(ctx, labKey); got != string(alert.StatusActive) {
		die(fmt.Sprintf("threshold-1 run: want the lab topic's head active, got %q", got))
	}
	if got := headStatus(ctx, jobRequestsKey); got != string(alert.StatusActive) {
		die(fmt.Sprintf("threshold-1 run: want the job_requests topic's head active, got %q", got))
	}
	if got := executorCapture.count("warn", partitioncountcontroller.AlertPartitionCount, labTopic.Name); got != 1 {
		die(fmt.Sprintf("threshold-1 run: want 1 WARN edge for the lab topic, got %d", got))
	}
	fmt.Println("  ✓ threshold-1 run published active heads with WARN edges")

	summary := readCheckSummary(ctx)
	if summary[iMetrics.MetricCheckTopicsEvaluated] < 2 ||
		summary[iMetrics.MetricCheckTopicsFailed] != 0 ||
		summary[iMetrics.MetricCheckPublishedAlerts] != summary[iMetrics.MetricCheckTopicsEvaluated] {
		die(fmt.Sprintf("threshold-1 run: want every evaluated topic published and none failed, got %v", summary))
	}
	fmt.Println("  ✓ check summary: every evaluated topic published, none failed")

	// inside the system's 4h repeat interval the same finding publishes nothing
	published := alertMessageCount(ctx, labKey)
	secondRun, err := mAdmin.RunCronJob(ctx, partitioncount.JobName, nil)
	must(err)
	waitDelivered(ctx, secondRun.Id, "success")
	if got := alertMessageCount(ctx, labKey); got != published {
		die(fmt.Sprintf("repeat-interval run: want no republish, got %d messages after %d", got, published))
	}
	if got := executorCapture.count("warn", partitioncountcontroller.AlertPartitionCount, labTopic.Name); got != 1 {
		die(fmt.Sprintf("repeat-interval run: want no new WARN, got %d", got))
	}
	fmt.Println("  ✓ a second run inside the repeat interval published nothing")

	summary = readCheckSummary(ctx)
	if summary[iMetrics.MetricCheckPublishedAlerts] != 0 ||
		summary[iMetrics.MetricCheckTopicsFailed] != 0 {
		die(fmt.Sprintf("repeat-interval run: want a quiet summary, got %v", summary))
	}
	fmt.Println("  ✓ check summary: the quiet run counted zero publishes")

	// exact-name dispatch: the live consumer never claims another job's
	// request, and alert traffic leaves other groups alone
	readCostRun, err := mAdmin.RunCronJob(ctx, compactionreadcost.JobName, nil)
	must(err)
	thirdRun, err := mAdmin.RunCronJob(ctx, partitioncount.JobName, nil)
	must(err)
	waitDelivered(ctx, thirdRun.Id, "success")
	if got := scalarInt64(ctx, fmt.Sprintf(
		`SELECT COUNT(*) FROM delivery_log_%d WHERE consumer_group_id = %d AND message_id = %d;`,
		jobRequests.Id, partitionCountGroup, readCostRun.Id)); got != 0 {
		die(fmt.Sprintf("the executor must not claim another job's request, got %d delivery rows", got))
	}
	for _, foreignGroup := range []int64{otherGroup, bindinglessGroup} {
		claimed := scalarInt64(ctx, fmt.Sprintf(
			`SELECT COUNT(*) FROM exception_queue_%d WHERE consumer_group_id = %d;`, jobRequests.Id, foreignGroup))
		logged := scalarInt64(ctx, fmt.Sprintf(
			`SELECT COUNT(*) FROM delivery_log_%d WHERE consumer_group_id = %d;`, jobRequests.Id, foreignGroup))
		if claimed != 0 || logged != 0 {
			die(fmt.Sprintf("group %d must be untouched by alert runs, got %d claims %d log rows", foreignGroup, claimed, logged))
		}
	}
	fmt.Println("  ✓ foreign request unclaimed, other/bindingless groups untouched")
}

func isolationSection(ctx context.Context) {
	step("isolation: a corrupted head fails its topic's Record, the others still resolve")

	labKey := partitionCountKey(labTopicOwner)
	jobRequestsOwner, err := common.NewTopicOwner(jobRequests.SystemId, jobRequests.Id, jobRequests.Name)
	must(err)
	jobRequestsKey := partitionCountKey(jobRequestsOwner)

	// the head row stays, but its payload no longer unmarshals into an Alert
	corruptedHead := headId(ctx, labKey)
	saved := scalarString(ctx, fmt.Sprintf(
		`SELECT payload::text FROM message_log_%d WHERE id = $1;`, alertsTopic.Id), corruptedHead)
	exec(ctx, fmt.Sprintf(
		`UPDATE message_log_%d SET payload = '"corrupt"'::jsonb WHERE id = $1;`, alertsTopic.Id), corruptedHead)

	declareThreshold(ctx, 0)
	resolveRun, err := mAdmin.RunCronJob(ctx, partitioncount.JobName, nil)
	must(err)

	// the attempt fails on the corrupted owner -- but the same attempt
	// already resolved every healthy topic
	waitDelivered(ctx, resolveRun.Id, "failure")
	if got := headStatus(ctx, jobRequestsKey); got != string(alert.StatusResolved) {
		die(fmt.Sprintf("isolation: healthy topics must resolve beside the failure, got %q", got))
	}
	if got := executorCapture.count("info", partitioncountcontroller.AlertPartitionCount, jobRequests.Name); got != 1 {
		die(fmt.Sprintf("isolation: want 1 resolve INFO for the healthy topic, got %d", got))
	}
	if got := headId(ctx, labKey); got != corruptedHead {
		die("isolation: the corrupted owner's head must not move")
	}
	fmt.Println("  ✓ healthy topics resolved in the same attempt the corrupted owner failed")

	// resolved is left unasserted here: an automatic retry may already have
	// overwritten the summary, and only its failed/published counts repeat
	summary := readCheckSummary(ctx)
	if summary[iMetrics.MetricCheckTopicsFailed] != 1 ||
		summary[iMetrics.MetricCheckPublishedAlerts] != 0 {
		die(fmt.Sprintf("isolation: the failed run must still produce its summary, got %v", summary))
	}
	fmt.Println("  ✓ check summary went out on the failed run: exactly 1 topic failed")

	// fixing the head lets the request's retry resolve the last owner
	exec(ctx, fmt.Sprintf(
		`UPDATE message_log_%d SET payload = $1::jsonb WHERE id = $2;`, alertsTopic.Id), saved, corruptedHead)
	waitDelivered(ctx, resolveRun.Id, "success")
	if got := headStatus(ctx, labKey); got != string(alert.StatusResolved) {
		die(fmt.Sprintf("isolation: want the fixed owner resolved on retry, got %q", got))
	}
	if got := executorCapture.count("info", partitioncountcontroller.AlertPartitionCount, labTopic.Name); got != 1 {
		die(fmt.Sprintf("isolation: want 1 resolve INFO for the fixed owner, got %d", got))
	}
	fmt.Println("  ✓ retry resolved the fixed owner; healthy topics resolved exactly once")

	summary = readCheckSummary(ctx)
	if summary[iMetrics.MetricCheckTopicsFailed] != 0 ||
		summary[iMetrics.MetricCheckResolvedAlerts] != 1 {
		die(fmt.Sprintf("isolation retry: want 0 failed and only the fixed owner resolved, got %v", summary))
	}
	fmt.Println("  ✓ retry summary: zero failed, only the fixed owner resolved")
}

// --- harness ---

// startExecutor claims the partition_count worker row and runs its execution
// until the returned stop is called.
func startExecutor(ctx context.Context) func() {
	provisioner, err := partitioncount.NewPartitionCountProvisioner(ds, &partitioncount.PartitionCountConfig{
		Logger: executorCapture,
	})
	must(err)
	workers, err := workercontroller.NewWorkerController(ds, nil)
	must(err)
	row, err := workers.GetWorker(ctx, partitioncount.JobName, groupOwner)
	must(err)
	if row == nil {
		die("the " + partitioncount.JobName + " worker row is missing")
	}

	// a crashed earlier run's claim lingers until its InstanceTTL expires --
	// retry past it instead of dying
	var execution worker.Execution
	deadline := time.Now().Add(60 * time.Second)
	for {
		execution, err = provisioner.Provision(ctx, row)
		must(err)
		if execution != nil {
			break
		}
		if time.Now().After(deadline) {
			die("the alert worker declined the instance for 60s -- is a daemon already running?")
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

// registerGroup creates a consumer group on the job_requests topic, bound to
// the given job names (none = bindingless), and returns its id.
func registerGroup(ctx context.Context, name string, bindings ...string) int64 {
	controller, err := consumergroupcontroller.NewConsumerGroupController(ds, nil)
	must(err)
	group, err := controller.RegisterGroup(ctx, jobRequests.Id, name, consumergroup.Beginning())
	must(err)
	_, err = controller.DeclareBindings(ctx, jobRequests.Id, group.Id, bindings, time.Now())
	must(err)
	return group.Id
}

func cleanup() {
	ctx := context.Background()

	must(mAdmin.UnsuspendCronJob(ctx, partitioncount.JobName))
	must(mAdmin.UnsuspendCronJob(ctx, compactionreadcost.JobName))

	labKey := partitionCountKey(labTopicOwner)
	checkKey, err := alert.MessageKey(labCheckName, labTopicOwner)
	must(err)
	keys := []string{labKey, checkKey}
	exec(ctx, fmt.Sprintf(`DELETE FROM compaction_head_%d WHERE compaction_key = ANY($1);`, alertsTopic.Id), keys)
	exec(ctx, fmt.Sprintf(`DELETE FROM message_log_%d WHERE message_key = ANY($1);`, alertsTopic.Id), keys)

	must(mAdmin.DestroyTopic(ctx, labTopic.Name, admin.DestroyOptions{Force: true}))

	for _, sql := range []string{
		fmt.Sprintf(`DELETE FROM exception_queue_%d WHERE consumer_group_id IN (SELECT id FROM consumer_group_config WHERE name LIKE '%s.%%');`, jobRequests.Id, prefix),
		fmt.Sprintf(`DELETE FROM delivery_log_%d WHERE consumer_group_id IN (SELECT id FROM consumer_group_config WHERE name LIKE '%s.%%');`, jobRequests.Id, prefix),
		fmt.Sprintf(`DELETE FROM claim_lease_%d WHERE consumer_group_id IN (SELECT id FROM consumer_group_config WHERE name LIKE '%s.%%');`, jobRequests.Id, prefix),
		fmt.Sprintf(`DELETE FROM consumer_group_config WHERE name LIKE '%s.%%';`, prefix),
	} {
		exec(ctx, sql)
	}
}

// --- capture logger ---

// captureLogger records every line so sections can count edges by their
// alert/owner attributes.
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

// count is the number of lines at level carrying alert=alertName owner=ownerName.
func (c *captureLogger) count(level string, alertName string, ownerName string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	matches := 0
	for _, line := range c.lines {
		if line.level == level && line.args["alert"] == alertName && line.args["owner"] == ownerName {
			matches++
		}
	}
	return matches
}

// --- assertion helpers ---

// readCheckSummary returns the partition_count check summary heads by metric
// name -- the latest run's counts, read the same way `vulkan metrics list`
// reads them.
func readCheckSummary(ctx context.Context) map[string]float64 {
	heads, err := mAdmin.ListMeasurements(ctx)
	must(err)
	attributes := map[string]string{"alert": partitioncountcontroller.AlertPartitionCount}
	byKey := make(map[string]float64, len(heads))
	for _, head := range heads {
		byKey[head.MessageKey] = head.Message.Value
	}
	summary := make(map[string]float64, 4)
	for _, name := range []string{
		iMetrics.MetricCheckTopicsEvaluated,
		iMetrics.MetricCheckTopicsFailed,
		iMetrics.MetricCheckPublishedAlerts,
		iMetrics.MetricCheckResolvedAlerts,
	} {
		value, ok := byKey[iMetrics.MeasurementKey(name, attributes)]
		if !ok {
			die(fmt.Sprintf("no check summary head for %s", name))
		}
		summary[name] = value
	}
	return summary
}

func partitionCountKey(owner *common.Owner) string {
	key, err := alert.MessageKey(partitioncountcontroller.AlertPartitionCount, owner)
	must(err)
	return key
}

func alertMessageCount(ctx context.Context, messageKey string) int64 {
	return scalarInt64(ctx, fmt.Sprintf(
		`SELECT COUNT(*) FROM message_log_%d WHERE message_key = $1;`, alertsTopic.Id), messageKey)
}

func headId(ctx context.Context, messageKey string) int64 {
	return scalarInt64(ctx, fmt.Sprintf(
		`SELECT head_id FROM compaction_head_%d WHERE compaction_key = $1;`, alertsTopic.Id),
		messageKey)
}

// headStatus is "" when the key has no head or its payload carries no status.
func headStatus(ctx context.Context, messageKey string) string {
	sql := fmt.Sprintf(`
		SELECT m.payload->>'status'
		FROM compaction_head_%d h
		JOIN message_log_%d m ON m.id = h.head_id
		WHERE h.compaction_key = $1;
	`, alertsTopic.Id, alertsTopic.Id)
	var status *string
	err := ds.Pool.QueryRow(ctx, sql, messageKey).Scan(&status)
	must(err)
	if status == nil {
		return ""
	}
	return *status
}

// waitDelivered returns once the partition_count group's delivery log holds
// the request at the given status.
func waitDelivered(ctx context.Context, messageId int64, status string) {
	waitForCount(ctx, fmt.Sprintf(
		`SELECT COUNT(*) FROM delivery_log_%d WHERE consumer_group_id = %d AND message_id = %d AND status = '%s';`,
		jobRequests.Id, partitionCountGroup, messageId, status), 1)
}

func waitForCount(ctx context.Context, sql string, want int64) {
	deadline := time.Now().Add(60 * time.Second)
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

func scalarString(ctx context.Context, sql string, args ...any) string {
	var value string
	must(ds.Pool.QueryRow(ctx, sql, args...).Scan(&value))
	return value
}

func exec(ctx context.Context, sql string, args ...any) {
	_, err := ds.Pool.Exec(ctx, sql, args...)
	must(err)
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
