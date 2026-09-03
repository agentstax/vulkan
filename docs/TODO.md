# TODO

Sliding window of in-flight work only. Future work lives in ROADMAP.md;
shipped work in HISTORY.md; decision rationale in DECISIONS.md ->
docs/decisions/.

## Consume runs the system manager [0635, amended]

Every long-running verb keeps the system's upkeep running. Two changes,
built together:

1. **Shared run**: the client's one `SystemManager` is started by the
   first active long-running verb (`Consume`, `RunManager`,
   `Schedule.Schedule`) and stopped by the last one out. The in-process
   permit and its "system manager already running" error are deleted.
2. **Gated manager row**: the system manager's `worker_config` row is
   declared with `target_instances = 1` instead of -1. One process in
   the deployment runs the reconcile loop; every other process's run
   loop keeps retrying the claim on its existing `RetryDelay` interval
   and takes over at TTL expiry. This replaces 0635's accepted risk
   ("idle reconcile polling scales with the consumer fleet").

Settled in design discussion (2026-09-02):

- No new column. `target_instances` already means: -1 no cap, 0
  suspended, N cap at N -- for every worker
  (`pkg/metrics/worker.go` WorkerStatus, VK0035's diagnose query).
  Suspend-vs-gated is a manager-run-loop inference bug, not a data
  gap: `Runner` labels every declined claim "suspended" because -1
  never declines. Fix the inference; a `status` column was considered
  and rejected (second home for a fact the column encodes; revisit
  only if suspend/unsuspend becomes a worker CLI verb pair and
  restoring the old cap annoys).
- Consumer group manager rows STAY -1. Each replica of a group must run
  its own reconcile loop to spawn its own message/exception consumer
  executions (consumer rows are themselves unbound -- the message claim
  is their gate). Only the system-owner manager row is gated.
- `ClientConfig.DisableManager` is the opt-out, client-wide, zero value
  keeps auto-run on. Explicit `RunManager` / `Schedule.Schedule` are
  unaffected by it -- running the manager is their stated purpose.
- Fatal loop exit with no caller: logged as a declared Error event, no
  automatic restart (a spawned worker returning fatal would replay).
  The next long-running verb to start brings the loop back.
- Errors before the loop starts (system-owner read fails, system not
  registered) return to the caller whose join starts the loop -- for a
  `Consume` that tears the session down with the real error. [0624]
  makes the not-registered case near-impossible for consumers.
- 0635's sentence "the in-process permit still refuses a second
  explicit RunManager" is superseded; needs decision record 0638 (next
  after current max 0637) linked both ways.

### Chunk 1 -- worker layer: expose the target, fix the decline inference -- DONE

Built 2026-09-02. Main module, cmd/vulkan, otelvulkan, examples, bench all
build; `go vet ./...` clean; `go test -race ./...` 120 passed / 78 packages;
tools 34 passed. No DB labs run yet (chunk 5 owns those).

- `worker.WorkerData` carries `TargetInstances int`
  `json:"target_instances"` with `// 0 = suspended; NoInstanceTarget = no
  cap`; both adapters (`toWorkerData`, `toOwnedWorker`) fill it. Both
  queries already selected the column -- only the adapters dropped it.
- `manager.Runner.claim` now classifies instead of the run loop guessing:
  target 0 warns VK0035 and skips the claim attempt outright (it could
  only decline); a decline under any other target logs Debug "manager row
  declined an instance -- retrying the claim" with `owner` and
  `target_instances`. `Run`'s switch has three arms, the middle one empty
  with the caption "nothing to run this life -- claim logged which case it
  was".
- Side effect of skipping the claim on target 0: a suspended row with
  unparseable metadata now idles quietly instead of returning
  `Provision`'s parse error, which pre-first-claim was fatal. Suspended
  rows should not crash the loop, so this is wanted -- but it is a real
  behavior change, not a no-op.
- `RunnerConfig.RetryDelay` doc comment now says it also paces attempts
  while the target is filled, so takeover lands within InstanceTTL +
  RetryDelay of the holder's exit, and scopes the >= InstanceTTL rule to
  unbound rows (a gated row's re-claim is refused by the gate).
- CONVENTIONS ## Logging registry gained the `target_instances` row.
- The `Runner` doc comment's caveat ("Safe only for manager rows: a
  target-gated worker re-claiming itself would take back an instance
  another claim just won") was REPLACED, not kept. It does not hold:
  `ErrInstanceLost` means the row is already gone or expired
  (`renewInstance` requires `expires_at > now()`), and the re-claim goes
  back through `claimInstance`'s count of unexpired rows. What is actually
  manager-specific is that no other manager spawns the manager -- the
  comment now says that. A gated-row takeover lab in chunk 5 is the
  empirical backstop.
- VK0035's docs page already says "target_instances 0" specifically, so
  the code now matches the page; no page edit needed for this chunk.

Verified during chunk 1, clearing chunk 2/5 items:

- `metrics/controller/adapter.go classifyWorker` is
  target 0 -> suspended / live > 0 -> claimed / else unclaimed, with no
  -1 special case, so a manager row at target 1 classifies exactly as it
  does at -1. The workerliveness risk is CLEARED (nothing to change).
- Nothing in labs, playground, bench, or the CLI reads or asserts
  `target_instances`; the only doc mentions are VK0035's page and
  schedules.mdx.
- schedules.mdx already reads "If a manager already runs in the fleet,
  `Schedule` is one more instance of it, and the worker's
  `target_instances` decides which one holds it" -- true only AFTER chunk
  2. The page is written for the gated world; today it is wrong.
- `manager run`'s CLI help ("Safe to run N-way: replicas coordinate
  through worker claims, so each worker's instance target holds") stays
  accurate and gets more so; chunk 4 still reviews it for the deleted
  refusal.
- `WorkerData` is publicly aliased (`vulkan.WorkerData`, returned by
  `Group.ListWorkers`), so the new field widens that read-model's json.
  Additive; the CLI's `group config get` reads only `Metadata`.
- No SQL literal moved, so the sandbox SQL mirror needs no re-sync.

### Chunk 2 -- gate the system manager row -- DONE

Built 2026-09-02. All six modules build, `go vet ./...` clean, `go test
-race ./...` 120 passed / 78 packages, tools 34 passed. Verified live
against the dev DB in a throwaway schema (below).

- `ManagerConfig.TargetInstances` added as the first (domain) field.
  WithDefaults 0 -> `worker.NoInstanceTarget`, so the consumer's
  provisioner keeps declaring unbound group manager rows without being
  touched; Validate rejects < -1, mirroring `WorkerConfig.Validate` and
  the DDL's own `CHECK (target_instances >= -1)`.
  `NewManagerProvisioner` writes `cfg.TargetInstances` onto the
  Definition instead of hard-coding -1.
- `pkg/admin/admin.go`'s declarer provisioner passes `TargetInstances: 1`
  -- this is the one that reaches `SystemController.Register`, so new
  installations declare the system manager row gated.
  `pkg/systemmanager/systemmanager.go` passes 1 as well so its Definition
  matches the row it claims.
- Prose corrected where gating made it false: `NoInstanceTarget`'s doc
  comment (was "like the manager"), `ManagerProvisioner`'s type comment
  (was "Manager rows carry no instance target"), `Provision`'s nil-return
  comment (was "target_instances was set away from NoInstanceTarget"),
  `SystemManager`'s type comment, and `manager run`'s CLI help (now says
  one replica holds the claim and the rest retry it).
- `systemmanager.go`'s permit comment no longer claims the row is
  unbound; it now says the row's gate caps the deployment while the
  permit caps the process. Chunk 3 deletes both.

Verified live (throwaway schema `vulkan_gatecheck`, dropped after; the
dev DB's own `vulkan` schema was never written to):

- CHECK 1: `RegisterSystem` declares the system manager row at
  `target_instances = 1`.
- CHECK 2: two clients each calling `RunManager` (two SystemManagers, so
  the permit is not involved -- only the row's gate) produce exactly ONE
  live manager instance.
- CHECK 3: on cancel, the instance is released, not left to expire (0
  live immediately).
- CHECK 4: with the row set to 0, VK0035 warns and nothing is claimed.
- The waiting runner logs exactly the new line, and NOT the old
  suspended Warn:
  `DEBUG manager row declined an instance -- retrying the claim
  owner=system target_instances=1`.

GOTCHA -- an existing installation keeps the old behavior. Confirmed on
the dev DB: its manager row (id 4) is still -1, because `registerWorker`
updates only metadata on re-declare. That is deliberate (an operator's
suspend must survive a restart), so seeing the gate requires a fresh
schema or a manual `UPDATE ... SET target_instances = 1`. Pre-v1 answer
is drop+recreate; say so in the HISTORY entry at close-out.

Observed while verifying, for chunk 5's labs: a single system manager
process legitimately runs SEVERAL manager instances -- one for the system
row plus one per alert consumer group (`owner=alert.partition_count` and
siblings), since each alert consumer runs its own group manager. A lab
counting manager instances must scope the count to the system row
(`system_id IS NOT NULL`), or it will read those as duplicates.

### Chunk 2b -- the instance target becomes vocabulary [0639] -- DONE

Built 2026-09-02 on the user's call (shape 2 of three offered). All six
modules build, vet clean, `go test -race ./...` passes, tools 34 passed,
and the live checks below re-ran green after the refactor.

- `worker.InstanceTarget` (named int) owns `NoInstanceTarget`, a
  `Suspended()` reader for the zero, and `Validate` (positive count or
  NoInstanceTarget; 0 rejected -- suspension is an operator's row edit,
  never a declaration).
- `NewDefinition(name, ownerKind, targetInstances, metadata)` takes it as
  a required param. Both post-construction pokes deleted: the manager
  passes `cfg.TargetInstances` in, and the three consumer kinds pass
  `NoInstanceTarget` themselves while `NewBaseProvisioner` guards it
  instead of stamping it.
- `NewManagerProvisioner(ds, targetInstances, cfg, provisioners...)` takes
  it as a required param too, and `ManagerConfig` drops the field: it was a
  required value hiding in a config (zero could not mean unset without
  meaning suspended, and the default it needed served 1 of 3 callers while
  being the expensive one -- a fourth caller who said nothing would have
  restored the fleet-wide polling 0638 removed).
- Type reaches WorkerData, WorkerConfig; the datastore row
  and `RegisterWorker` keep the column's plain int, adapters convert.
  `metrics.WorkerSnapshot` stays int on purpose -- a vocabulary root
  imports infrastructure only, never another domain.
- Record 0639 written, DECISIONS indexed, 0549 back-linked.

Live re-verification (throwaway schema, dev DB untouched): system manager
row declared at 1; a registered consumer's `message_consumer` row at -1
(also exercises the new base guard); two runners -> one live instance; a
clean release on stop; target 0 -> VK0035 and nothing claimed.

### Chunk 3 -- SystemManager Run rework -- DONE

Built 2026-09-02, then STRIPPED the same day on review [0641]. All
modules build, vet clean, `go test -race ./...` passes, tools 34 passed,
website codes.test.ts 6 passed. Live-verified under `-race` (below).

- The permit and its "system manager already running" refusal are gone,
  along with the `concurrency` import. Nothing is refused.
- The plan's join-and-block shape (mutex-guarded caller count, shared
  loop) was built, then deleted on review: the count had no users (the
  scheduler builds a SystemManager per Schedule call; RunManager is
  called once everywhere), and N in-process callers are exactly what
  `target_instances = 1` already arbitrates. [0641] supersedes 0638's
  clause; `SystemManager` holds only its dependencies.
- `Run` = resolve system owner + build a `manager.Runner` (both
  synchronous, so misconfiguration fails the caller), then run it under
  the caller's own ctx inside the re-claim/backoff loop.

DEVIATION from the plan, recorded as [0640] superseding 0638's clause:
the loop RE-CLAIMS behind a backoff instead of ending. No-restart would
have shipped a regression -- one fatal would kill a process's upkeep for
its whole lifetime and leave `vulkan manager run` up doing nothing.

- `SystemManagerConfig.RunRetry` is the per-loop curve (the
  SweepRetry/TickRetry convention), defaulted and validated like the rest.
- VK0065 declared unexported in `pkg/systemmanager/logs.go` (the VK0041
  precedent for an event in an API package); page written; codes.json
  regenerated.
- Stale "second concurrent run is refused" doc comments fixed in
  client.go RunManager, schedule.go Schedule, scheduler_instance.go.

Live verification under `-race` (throwaway schema, dev DB untouched):
two `Run` calls on ONE client are both admitted and share one instance
row; when the claiming caller cancels, the remaining caller takes over
within RetryDelay; the last one out releases the claim (0 live
immediately, not left to expire); a later `Run` claims fresh.

GOTCHA found while wiring the docs: a new declaring package must be
linked into BOTH `tools/codeexport/main.go` and
`tools/conventions/conventions.go`. Neither list had `pkg/systemmanager`,
and `TestRegistryCoversEverySourceCode` did NOT catch it -- that test
passes only because `seams_test.go` imports `pkg/vulkan`, which links
systemmanager transitively into the test binary. The real net is
`codes.test.ts` ("gives every page a declaration"), which would have
failed on the orphaned VK0065 page. Both lists now name systemmanager.

### Chunk 4 -- client and consumer wiring

- `pkg/vulkan/client_config.go`: `DisableManager bool`, doc comment
  naming the two deployments that set it -- dedicated
  `vulkan manager run` processes, and consumers under a database role
  without DDL rights (the topic janitor runs DDL).
- `pkg/consumer/consumer_config.go`: new optional field
  `RunSystemManager func(ctx context.Context) error` -- runs beside
  the session until ctx cancels; nil means this process runs no
  system manager. Note: the config's doc comment says the config is
  "the group's declaration"; this field is process-shaped -- amend
  that comment honestly (session/process fields already exist in
  spirit via Logger/Retry) or find a better seam; do not silently
  contradict it.
- `pkg/vulkan/adapter.go` `toConsumerConfig`: fill it with
  `c.manager.Run` unless `cfg.DisableManager`; signature grows the
  needed input. (`toConsumerConfig` takes the client's resolved
  values, so thread the func through the same way Logger/Retry go.)
- `pkg/consumer/consumer_instance.go` `Consume`: when the field is
  non-nil, add `group.Go(func() error { return i.Config.RunSystemManager(runCtx) })`
  beside the metrics and runner members. Blocks until runCtx cancels,
  returns nil then; a pre-loop error tears the session down with the
  real error (wanted). Add the fact to the start line:
  `"disable_manager", i.Config.RunSystemManager == nil`.
- `pkg/vulkan/client.go` `RunManager`: body unchanged
  (`c.manager.Run(ctx)`); rewrite the doc comment -- no refusal;
  joins the running loop or starts it; claim arbitration across
  processes.
- `pkg/vulkan/schedule.go` `Schedule.Schedule`: body unchanged;
  same comment rewrite.
- `cmd/vulkan/internal/cli/manager_run.go`: check help text for
  refusal/second-run wording.

### Chunk 5 -- examples, labs, docs

- `examples/playground/08-manager-and-consumer/main.go`: collapses to
  a plain Consume; rewrite the header (concept count drops; the traps
  it documents are the ones this work deletes).
- Review `examples/playground/12-metrics-read` and
  `13-alert-consumer`: both call `RunManager`; keep or collapse per
  what each scenario teaches.
- Labs (targeted, foreground per change; full fresh-DB suite at the
  review-ready checkpoint):
  - two registered consumers on one client, both consuming: exactly
    one system manager instance row; both sessions end -> row
    released (not expired).
  - two clients/processes: one holds the manager claim, cancel its
    session, the other claims within TTL + RetryDelay.
  - `target_instances = 0` on the manager row: VK0035 Warn, loop
    idles, no reconcile.
  - `DisableManager: true`: no system manager instance row appears;
    start line shows `disable_manager=true`.
  - explicit `RunManager` + a Consume in one process: one loop, one
    instance row; RunManager's ctx cancel does not stop upkeep while
    the Consume lives.
  - fatal loop exit: VK0065 logged once, Consume unaffected, next
    Consume restarts the loop.
- Grep labs for hand-copied manager/worker queries before moving any
  production query.
- Doc site (shipped-behavior updates, same work): client guide "The
  manager" section (auto-run, DisableManager, the gate as the
  runtime dial: 0 suspends deployment-wide, 1 default, N copies, -1
  every-process), the schedule section's "running every worker
  without having said so" paragraph, quickstart CAUTION aside,
  VK0035 page (gated-decline is not suspension), new VK0065 page.
- Decision record 0638; flip the superseded sentence of 0635 with
  links both ways. HISTORY entry at close-out; drop this section.
