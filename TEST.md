# TEST

Draft record of the shutdown/interruption scenarios exercised against
`pkg/producer`/`pkg/consumer` (dev Postgres, :5432) after the ShutdownFunc-hook
removal. Written from the scratch harness used to verify them, not yet real
code -- the intent is to promote this into an actual `pkg/producer`/
`pkg/consumer` test suite once the API stops moving. Each entry is Setup /
Action / Assert so it transcribes directly into a test function later.

Fixture shared by every case unless noted: one `PostgresDatastore` against
`example_db`, a fresh topic per case (`topic.Register` + `topic.Destroy` in
teardown), `Work{Id string}` as the message type.

## Producer lifecycle & shutdown

### P1 -- idle lifecycle cancel refuses the next Produce
Setup: Register a producer with a cancellable lifecycle ctx.
Action: cancel the lifecycle ctx while idle (no in-flight `Produce`), then call `Produce`.
Assert: returns before enqueueing; `errors.Is(err, vulkanerrors.ErrShutdownRequested)`.

### P2 -- lifecycle cancels mid-flight, call's own ctx stays alive -> still commits
Setup: Register, `BatchConcurrencyLimit: 1`.
Action: start a `Produce` call in a goroutine, sleep briefly so it's enqueued and past the gate, cancel the lifecycle ctx (not the call's ctx) before it resolves.
Assert: the in-flight call still returns nil error -- "queued messages still commit, new ones are refused" holds.

### P3 -- call's own ctx cancels mid-wait -> resolves within BatchShutdownGrace
Setup: Register, `BatchAttemptTimeout: 2s`, `BatchShutdownGrace: 5s`.
Action: start `Produce` with a call ctx, cancel that ctx ~20ms later (lifecycle stays alive).
Assert: returns nil error -- the batch still committed and the grace window absorbed the cancellation.

### P4 -- negative BatchShutdownGrace abandons immediately
Setup: Register, `BatchShutdownGrace: -1`.
Action: call `Produce` with an already-cancelled ctx.
Assert: returns an ambiguous-abandon error in well under a second (no grace wait at all).

### P5 -- already-cancelled ctx is rejected before enqueue
Setup: Register.
Action: call `Produce` with a pre-cancelled ctx, then call `Produce` again with a fresh ctx (probe).
Assert: first call errors before enqueue; probe call succeeds -- confirms the rejected call published zero rows and didn't wedge the batcher.

### P6 -- fire-and-forget (Background + DisableGracefulShutdown)
Setup: `MessageProducerConfig{DisableGracefulShutdown: true}`.
Action: `Register(context.Background())`, then `Produce`.
Assert: Register succeeds despite the non-cancellable ctx; Produce succeeds.

### P7 -- Register twice while live
Setup: Register with a cancellable ctx (do not cancel).
Action: call `Register` again with a different ctx.
Assert: `errors.Is(err, vulkanerrors.ErrAlreadyRegistered)`, message references "the context from the first Register still owns this producer's shutdown".

### P8 -- Register after wind-down
Setup: Register, then cancel the lifecycle ctx.
Action: call `Register` again.
Assert: `ErrAlreadyRegistered`, message contains "wound down and stays down".

### P9 -- Produce before Register
Setup: construct a producer, do not call Register.
Action: call `Produce`.
Assert: `errors.Is(err, vulkanerrors.ErrNotRegistered)`.

### P10 -- Register(Background) without the opt-out
Setup: default config (`DisableGracefulShutdown` false).
Action: `Register(context.Background())`.
Assert: `errors.Is(err, vulkanerrors.ErrLifecycleContextNotCancellable)`; error text includes the `LifecycleContext` teaching snippet.

### P11 -- N concurrent Produce calls all resolve on lifecycle cancel, none hang
Setup: Register, `BatchAttemptTimeout: 2s`, `BatchShutdownGrace: 3s`.
Action: fire 20 concurrent `Produce` calls on a shared ctx, cancel the lifecycle ctx ~50ms in, wait on all 20 with a hard timeout.
Assert: all 20 resolve (each either commits or returns `ErrShutdownRequested`) well inside the timeout -- nothing hangs.

### P12 -- caller-keyed Produce respects the same gate
Setup: Register, then cancel the lifecycle ctx.
Action: call `Produce` with `ProduceOptions{IdempotencyKey: <v7>}` (routes to the per-call path, bypassing the batcher).
Assert: `ErrShutdownRequested` -- the gate applies uniformly regardless of which internal path handles the call.

### P13 -- SIGKILL mid-run (documented, not "fixed")
Setup: run the producer as a real subprocess, `LifecycleContext()`-based, producing on a loop.
Action: `kill -9` the process ~1.3s after start.
Assert: process dies immediately; Postgres shows zero `pg_prepared_xacts` and zero ungranted `pg_locks` afterward -- the in-flight transaction rolled back cleanly on connection drop, no corruption, no orphaned state. Nothing to fix; confirms the documented tradeoff (uncommitted work vanishes, committed work stays).

### P14 -- real SIGTERM via LifecycleContext()
Setup: subprocess using `vulkanctx.LifecycleContext()`, producing on a loop.
Action: `kill -TERM` ~1s after start.
Assert: process exits 0 within ~150ms; log shows the in-flight message committed and the next call refused with `ErrShutdownRequested`.

### MISC -- batcher worker goroutine does not leak after idle
Setup: Register, produce 10 messages serially.
Action: sleep past the point the queue should have drained.
Assert: `runtime.NumGoroutine()` returns to (near) baseline -- the worker goroutine spawned on first enqueue exits once the queue empties, no `Close()` needed.

## Consumer lifecycle & shutdown

### C1 -- idle lifecycle cancel -> Consume returns nil promptly
Setup: Register a consumer with a cancellable lifecycle ctx, start `Consume` in a goroutine (no messages produced).
Action: cancel the lifecycle ctx after a short settle.
Assert: `Consume` returns nil within a few hundred ms.

### C2 -- lifecycle cancels mid-work -> in-flight consumerFunc finishes first
Setup: Register, `WorkTimeout: 3s`; seed one message; `consumerFunc` sleeps 600ms and flips a `finished` flag.
Action: cancel the lifecycle ctx once the handler is confirmed running (not before).
Assert: `Consume` returns nil AND `finished` is true -- the in-flight handler ran to completion, `Consume` didn't return out from under it.

### C3 -- call-ctx-only cancel (lifecycle alive) -> instance still usable ("restart loop")
Setup: Register, do not cancel lifecycle.
Action: run `Consume` with call-ctx #1, cancel it, confirm nil return; run `Consume` again on the SAME instance with a fresh call-ctx #2, cancel it too.
Assert: both sessions return nil -- one instance supports repeated `Consume(ctx, ...)` calls as long as the lifecycle stays alive.

### C4 -- lifecycle and call ctx cancel simultaneously
Setup: Register, start `Consume` with its own call ctx.
Action: cancel both the lifecycle ctx and the call ctx back-to-back.
Assert: still returns nil (no panic/race from the double-cancel).

### C5 -- Consume before Register
Setup: construct a consumer, do not Register.
Action: call `Consume`.
Assert: `errors.Is(err, vulkanerrors.ErrNotRegistered)`.

### C6 -- Register twice while live
Setup: Register with a cancellable ctx (do not cancel).
Action: call `Register` again.
Assert: `ErrAlreadyRegistered`, "the context from the first Register still owns this consumer's shutdown".

### C7 -- Register after wind-down
Setup: Register, then cancel the lifecycle ctx.
Action: call `Register` again.
Assert: `ErrAlreadyRegistered`, "wound down and stays down".

### C8 -- public Janitor(ctx) stops independently
Setup: Register a consumer (do not call Consume).
Action: run `Janitor(ctx)` with its own ctx, cancel that ctx (lifecycle untouched).
Assert: `Janitor` returns nil; lifecycle ctx is unaffected (confirms it's a standalone entrypoint, not coupled to Consume).

### C9 -- Consume + standalone Janitor share one lifecycle -> both stop together
Setup: Register once.
Action: run `Consume(ctx, ...)` and `Janitor(ctx)` concurrently (both on the shared package ctx), cancel the lifecycle ctx.
Assert: both return nil -- one lifecycle cancel stops every loop hanging off it, whether started via `Consume` or a standalone `Janitor` call.

### C10 -- hung consumerFunc past WorkTimeout+Grace -> hard-abandoned, Consume still exits
Setup: Register, `WorkTimeout: 300ms`, `WorkTimeoutGrace: 100ms`; seed one message; handler blocks forever on an unbuffered receive, ignoring ctx entirely.
Action: confirm the handler started, wait past `WorkTimeout+Grace` so the hard-abandon fires, then cancel the lifecycle ctx.
Assert: log shows "consumerFunc hard timeout, goroutine abandoned"; `Consume` returns nil promptly -- it does not block on the permanently-hung goroutine (by design: Go has no goroutine kill, so it's abandoned/leaked, not waited on).

### C11 -- consumerFunc panic is recovered, not fatal
Setup: Register, `MaxAttempts: 2`; seed two messages, "boom" and "fine".
Action: handler for "boom" does a nil-map write (panics); handler for "fine" just marks a flag.
Assert: both flags eventually flip (panic recovered as an exception, doesn't kill the loop); the sibling message still gets processed; process never crashes; `Consume` returns nil after lifecycle cancel.

### C12 -- Register(Background) without the opt-out
Setup: default config.
Action: `Register(context.Background())`.
Assert: `ErrLifecycleContextNotCancellable` with the teaching snippet.

### C13 -- opt-out works (Consume's own ctx becomes the only off-switch)
Setup: `MessageConsumerConfig{DisableGracefulShutdown: true}`.
Action: `Register(context.Background())`, run `Consume` with a cancellable call ctx, cancel it.
Assert: Register succeeds; `Consume` returns nil on the call ctx cancelling (since the lifecycle ctx itself can never cancel).

### C14 -- shared PostgresDatastore survives a consumer's wind-down
Setup: one shared `PostgresDatastore`; Register + run `Consume` to completion (lifecycle cancel -> nil return).
Action: after that consumer instance has fully wound down, construct a NEW producer on the SAME datastore and call `Produce`.
Assert: succeeds -- regression test for the shared-pool-closed bug fixed this session (`PostgresDatastore.Close()` is app-owned now, not called by consumer wind-down).

### C15 -- real SIGTERM via LifecycleContext()
Setup: subprocess consumer using `vulkanctx.LifecycleContext()`, background producer trickling messages.
Action: `kill -TERM` ~1.5s after start.
Assert: process exits 0 within ~150ms.

### C15b -- double-signal escalation does NOT force-kill (gap, see Findings)
Setup: subprocess consumer, `-hang` mode: `WorkTimeout: 20s`, `WorkTimeoutGrace: 1s`, handler ignores ctx and blocks forever once it picks up a message.
Action: send SIGTERM once the handler is confirmed hung; wait 2s; send SIGTERM again; wait 2s.
Assert (current, undesired): process is still alive after the second signal -- it has no observable effect. Process eventually self-bounds and exits 0, but only via `WorkTimeout+Grace` (~21s), never faster. See Findings.

### C16 -- restart loop terminates via the gate after wind-down
Setup: Register, cancel the lifecycle ctx immediately (before ever calling Consume).
Action: call `Consume`.
Assert: `errors.Is(err, vulkanerrors.ErrShutdownRequested)` -- a caller looping "call Consume, restart on return" terminates on the next call instead of hot-looping, because the entry gate itself refuses once wound down.

### C17 -- backend connection killed mid-claim (FIXED, was flaky -- see Findings)
Setup: Register, `ClaimPollRate: 100ms`; a background producer trickles messages.
Action: once `Consume` is idle-polling, run `SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = current_database() AND pid <> pg_backend_pid()` against the SAME shared datastore; then produce one more message and wait for it to be processed.
Assert (as of the `IsTransientPgError` fix below): `Consume` always recovers transparently and returns nil -- no more ~50% chance of dying outright.

### MISC -- N=6 repeat of C17 to characterize the failure rate (historical, pre-fix)
Action: run C17 six times back to back.
Result observed at the time: 3 recovered silently, 3 died with `FATAL: terminating connection due to administrator command (SQLSTATE 57P01)` (deaths confirmed at ~100ms post-kill, not a slow-backoff artifact -- `IsTransientPgError` never even attempted a retry for SQLSTATE 57P01 before the fix).

### MISC -- N=8 repeat of C17 post-fix
Action: run C17 eight times back to back after the `IsTransientPgError` change.
Assert: 8/8 recover transparently (20ms-1.1s each, all `Consume` returns nil) -- no deaths.

### MISC -- IsTransientPgError classification, all three new codes
Setup: none (pure function, no DB).
Action: call `retry.IsTransientPgError` with a synthetic `*pgconn.PgError` for each of `40P01`, `57P01`, `57P02`, `57P03` (expect retryable) and `42P01`, `42P07`, `23514`, `23505` (expect NOT retryable).
Assert: all eight match expectation -- the new codes are retryable, deterministic/permanent codes are untouched.

### MISC -- IsTransientPgError classification, full SQLSTATE sweep
Setup: none (pure function, no DB).
Action: call `retry.IsTransientPgError` with a synthetic `*pgconn.PgError` for every code considered in the follow-up audit -- retryable: `40P01`, `40001`, `08000`, `08001`, `08003`, `08006`, `08007`, `40003`, `53300`, `57P01`, `57P02`, `57P03`, `57P05`, `57014`. NOT retryable (checked deliberately, not by omission): `08004`, `08P01`, `40002`, `53000`, `53100`, `53200`, `53400`, `57P04`, `58000`, `58030`, `58P01`, `58P02`, `XX000`, `XX001`, `XX002`, `25P02` (already handled as post-failure noise by the batch resolver), plus the original sanity set (`42P01`, `42P07`, `23514`, `23505`).
Assert: all 32 codes classify as expected.

### MISC -- dropPartition survives a retry-after-ambiguous-commit
Setup: `CREATE TABLE IF NOT EXISTS scratchcheck_droptest`.
Action: `DROP TABLE IF EXISTS scratchcheck_droptest` twice in a row (the second call simulates a retry that doesn't know the first one already landed).
Assert: both calls succeed -- no `undefined_table` error on the second, unlike the bare `DROP TABLE` this replaced.

## Findings

1. **FIXED -- claim-loop connection kill was race-dependent fatal.** `retry.IsTransientPgError` didn't classify SQLSTATE 57P01/57P02/57P03 (admin/crash shutdown, cannot-connect-now) as retryable, so a connection killed mid-query became a `PermanentError` and took down whichever loop hit it -> `Consume` returned a raw pgconn error instead of nil, ~50% of the time in testing. Fixed by adding those three codes to `IsTransientPgError` alongside `40P01`, after auditing all ~21 `DatastoreRetry.Wrap` call sites across consumer/producer/topic/metrics for retry-safety-under-ambiguous-commit (every write is either self-consuming on re-entry or idempotency-key protected). Surfaced and fixed one pre-existing latent gap along the way: consumer's `dropPartition` used a bare `DROP TABLE` (no `IF EXISTS`), the one DDL call site in the codebase that wasn't already idempotent under retry. See TODO.md for the full writeup. Verified: 8/8 repeat connection-kill runs now recover transparently (was ~3/6); classification + dropPartition idempotency checked directly.

2. **No signal-escalation affordance (not yet fixed).** A second SIGTERM/Ctrl-C during a stuck graceful shutdown is silently swallowed -- `signal.NotifyContext`'s docs are explicit that default (force-kill) behavior only returns once `stop()` runs, and every example (`defer stop()`) defers that until after the blocking call already returned. Only the config's own `WorkTimeout`/`WorkTimeoutGrace` bounds a stuck shutdown; there's no faster manual override short of `SIGKILL -9`. Other libraries (asynq's SIGTSTP-then-SIGTERM, gRPC's `GracefulStop` + timer-triggered `Stop`) ship this as a deliberate two-signal idiom. Not filed as a TODO yet -- it's a new affordance, not a regression, and needs a design decision (own timer? re-arm `stop()` early?) before it's worth a queued item.
