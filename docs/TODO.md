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

### Chunk 2 -- gate the system manager row

- `pkg/worker/manager/manager_config.go`: add optional
  `TargetInstances int` to `ManagerConfig`. WithDefaults: 0 ->
  `worker.NoInstanceTarget` (today's behavior; consumers pass nothing
  and stay unbound). Validate: reject 0 explicitly set is impossible
  to distinguish from unset -- so legal values after defaults are -1
  or >= 1. `NewManagerProvisioner` writes it onto the Definition.
- `pkg/admin/admin.go`: the declarer ManagerProvisioner ("a declarer
  here, never run") passes `TargetInstances: 1`. This is what
  `SystemController.Register` declares, so new installations get a
  gated row.
- `pkg/systemmanager/systemmanager.go`: its own ManagerProvisioner
  passes `TargetInstances: 1` too, for a matching Definition (it never
  declares today, but the Definition should not lie).
- `pkg/consumer/consumer_provisioners.go`: unchanged -- group manager
  rows keep the unbound default.
- Existing databases: `registerWorker` updates only metadata on
  re-declare, so an existing manager row keeps -1 and keeps today's
  every-process behavior. Pre-v1 policy is drop+recreate; note it in
  HISTORY at close-out. No DDL change at all -- the target is an
  INSERT value.
- `pkg/worker/definition.go` / `worker_config.go` comment touch-ups
  where they describe the manager as the -1 case.
- workerliveness: CLEARED in chunk 1, no change needed
  (`classifyWorker` has no -1 case). Separately noted, pre-existing and
  NOT this work's job: the alert filters on `snapshot.Owner.TopicId`,
  so the SYSTEM manager row is outside every topic's check -- "nobody
  runs system upkeep" is unalertable from inside, since the check runs
  under the manager it would report on. VK0063 at register time stays
  the answer.

### Chunk 3 -- SystemManager shared run

`pkg/systemmanager/systemmanager.go`:

- Delete the `permit` field and the `concurrency` import; delete the
  "system manager already running" refusal.
- Replace with one mutex-guarded state on the struct: count of active
  `Run` calls, whether the loop goroutine is running, the loop's
  cancel func and done channel.
- `Run(ctx)` becomes join-and-block, same signature:
  1. Under the mutex: count++. If no loop is running: resolve
     `SystemOwner` and build the `manager.Runner` synchronously --
     on error, count--, return the error -- then start the loop
     goroutine under an internal cancellable context (derived from
     `context.Background()`; it must outlive any one caller).
  2. Block on `ctx.Done()`.
  3. On the way out, under the mutex: count--. If count is 0 and a
     loop is running: cancel it and wait on its done channel before
     returning (last-out drains -- `ReleaseInstance` deletes the
     instance row so a replacement or another process claims
     immediately instead of waiting out `expires_at`).
  4. Return nil (a requested stop).
- The loop goroutine: `runner.Run(loopCtx)`; on return, under the
  mutex mark not-running; a non-nil error (fatal -- a spawned worker
  declared itself unrunnable; runner returns nil on cancel) is logged
  as the new declared Error event. Never returned: no caller owns it.
- New `pkg/systemmanager/logs.go`: declared Error event, code VK0065
  (verify max across both registries at build time), message per the
  grammar (e.g. "system manager stopped -- upkeep is not running"),
  consequence fixed at declaration; call site attaches `"code"`,
  `"error"`. Docs page in the same change; re-sync the website
  codes.json registry.
- A join that finds count > 0 but no loop running (a fatal happened
  earlier) starts a fresh loop -- the step-1 branch already covers it;
  make sure the mutex ordering makes it true. Races to test: join
  while last-out is draining (must wait for the old done before
  starting anew, or serialize under the mutex), fatal exit racing a
  leave.
- `SystemOwner` is resolved per loop start, not per join.

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
