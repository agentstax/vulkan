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

### Chunk 1 -- worker layer: expose the target, fix the decline inference

- `pkg/worker/worker.go`: add `TargetInstances int` to `WorkerData`
  with json tag `target_instances` (public read-model field rule). The
  queries already select the column (`ListWorkersRow` embeds
  `WorkerConfigRow`; `getWorker` selects it) -- only the adapters drop
  it: fill it in `toWorkerData` and `toOwnedWorker`
  (`pkg/worker/controller/adapter.go`).
- `pkg/worker/manager/runner.go`: `Run` currently calls a `claim`
  helper that hides the row; restructure so `Run` holds the row
  (`GetWorker`, then `Provision`) and can branch on a declined claim:
  - `TargetInstances == 0` -> Warn VK0035 `EventManagerRowSuspended`,
    exactly today's behavior.
  - `TargetInstances > 0` -> Debug (another process holds the claim;
    keep waiting). Static message per the grammar, e.g. "manager
    instances at target -- waiting to claim". Debug never declares.
  - `-1` cannot decline; if it somehow does, treat as the Debug arm.
  Keep the one-value-plus-error rule in mind: prefer inlining over a
  three-value helper.
- `RetryDelay` (default 30s, jittered) is now also the waiting
  process's claim retry interval -- update its doc comment in
  `runner_config.go`.
- Re-examine and reword the `Runner` doc comment "Safe only for
  manager rows: a target-gated worker re-claiming itself would take
  back an instance another claim just won." Verified this session:
  `renewInstance` requires `expires_at > now()`, so a lost claim's row
  is expired or gone and the re-claim goes back through the count gate
  (which counts only unexpired rows) -- no take-back. Confirm with a
  test before deleting the caveat.

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
- Check `pkg/alert/workerliveness` classification against a gated
  manager row: with target 1 and no live instance the row is
  "unclaimed" -- same as -1 with no live instance today, but verify
  the alert's query/classify path doesn't special-case -1 before
  assuming no behavior change.

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
