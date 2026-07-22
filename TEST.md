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

### C15b -- double-signal escalation forces exit past a stuck handler (FIXED)
Setup: subprocess consumer, `-hang` mode: `WorkTimeout: 20s`, `WorkTimeoutGrace: 1s`, handler ignores ctx and blocks forever once it picks up a message.
Action: send SIGTERM once the handler is confirmed hung; wait 2s; send SIGTERM again.
Assert: process exits immediately (code 128+15) on the second signal, never waiting for `WorkTimeout+Grace` -- `LifecycleContext`'s own goroutine blocks on a second `<-sigs` independently of whatever `Consume` is doing and force-exits via `os.Exit` regardless of whether the stuck call ever returns. Confirmed this session via a minimal `LifecycleContext` harness (60s stuck call, two SIGTERMs 1s apart): exits in ~1s, code 143.

### C16 -- restart loop terminates via the gate after wind-down
Setup: Register, cancel the lifecycle ctx immediately (before ever calling Consume).
Action: call `Consume`.
Assert: `errors.Is(err, vulkanerrors.ErrShutdownRequested)` -- a caller looping "call Consume, restart on return" terminates on the next call instead of hot-looping, because the entry gate itself refuses once wound down.

### C17 -- backend connection killed mid-claim (FIXED, was flaky)
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

## Retry classification (pkg/retry) -- one entry per SQLSTATE

The classification checks above only prove `IsTransientPgError` returns the
right bool for a synthetic error value -- they never prove a retry actually
RUNS and SUCCEEDS through `DatastoreRetry.Wrap` for that code. That's the gap
this section closes: one test per retryable SQLSTATE, so none of the 14 get
forgotten. Grouped by how the error is actually produced, since that's the
part most likely to get skipped or faked wrong.

### Directly triggerable against the live dev Postgres (admin SQL, no extra infra)

#### RETRY-40P01 -- deadlock_detected
Setup: two `ProduceInTx` callers, each taking locks on two `CompactionKey`s' `latest_key` rows in REVERSE order of each other (`ProduceInTx` doesn't get the batch path's lock-order sort -- that's the one place a real deadlock can still happen, per `ProduceInTx`'s own doc comment).
Action: run both concurrently.
Assert: Postgres deadlocks one of them (`40P01`); `Wrap` retries it; both callers eventually succeed; `latest_key` ends up pointing at whichever committed last.

#### RETRY-53300 -- too_many_connections
Setup: a `PostgresDatastore` with `MaxConns: 1`; hold that one connection open manually via `pool.Acquire` (don't release it yet).
Action: start a `Produce`/claim call that needs the pool concurrently, then release the held connection after a short delay, still inside the retry budget.
Assert: the blocked call succeeds once the connection frees -- it retried through the exhaustion window instead of failing immediately.

#### RETRY-57014 -- query_canceled (external only)
Setup: Register a consumer, seed messages, start `Consume`.
Action: from a separate admin connection, find the consumer's backend pid in `pg_stat_activity` and call `pg_cancel_backend(pid)` (NOT `pg_terminate_backend`) exactly once mid-claim.
Assert: the claim retries and the consumer keeps processing with no loss -- AND confirm this is never misattributed as "caller context cancelled" in the reason log, since it's an external cancel, not ours (the thing the code comment's `context.Canceled` note depends on).

#### RETRY-57P01 -- admin_shutdown
Already covered live: see C17 and its N=8 post-fix repeat above. No new entry needed.

#### RETRY-57P05 -- idle_session_timeout
Setup: acquire a raw `pgx.Conn` (not through the pool, so its identity is stable), `SET idle_session_timeout = '50ms'` on that session.
Action: leave the connection idle past the timeout, then run a claim/produce that happens to reuse it.
Assert: the operation retries and succeeds on a fresh connection. Flag: fiddly -- `pgxpool` doesn't expose per-connection identity/GUC control from `Wrap`'s level, so this needs a raw connection outside the normal pool path to force the timeout onto a specific session. Lowest priority of the "directly triggerable" group.

### Needs network fault-injection infra (not built yet)

#### RETRY-08000 -- connection_exception
#### RETRY-08006 -- connection_failure
Both need a fault-injection proxy (e.g. toxiproxy) between the app and Postgres to sever the TCP connection mid-statement in a way that surfaces as a *formatted* PgError rather than a raw network error `pgconn.SafeToRetry`/`net.Error` already catch below the PgError branch. Candidate for a future toxiproxy-based lab. Until that exists, fall back to the synthetic-injection shape used for the unreachable group below (construct a fake `*pgconn.PgError` with the code, feed it through a real `DatastoreRetry.Wrap` call with a counter closure, assert it retries and returns nil on the second attempt) -- that at least proves the MECHANISM, even without proving the trigger is realistic.

#### RETRY-08001 -- sqlclient_unable_to_establish_sqlconnection
Setup: point a `PostgresDatastore` at the real dev Postgres, then stop the container/process briefly (needs control over the dev Postgres's lifecycle -- not currently something any lab does).
Action: attempt an operation while unreachable, restart Postgres before the retry budget exhausts.
Assert: the operation succeeds once the server is back. Infra gap: needs a lab that owns the Postgres container's lifecycle instead of assuming it's always up at `:5432`.

#### RETRY-08003 -- connection_does_not_exist
Genuinely hard to trigger deliberately -- `pgxpool` validates a connection's health before handing it out, so getting it to hand you one that's ALREADY dead needs reaching underneath pgx (e.g. closing the raw `net.Conn` while pgx still believes it's healthy). Synthetic-injection test only, same shape as the 08000/08006 fallback above; flag as possibly not worth a live-trigger attempt at all.

### Needs a disposable, independently controllable Postgres instance (not built yet)

#### RETRY-57P02 -- crash_shutdown
Requires actually crashing the Postgres server process (`kill -9` the postmaster, or worse). Destructive -- must never run against the shared dev Postgres other work depends on. Needs its own throwaway container spun up and torn down just for this test. Until that harness exists: synthetic-injection only.

#### RETRY-57P03 -- cannot_connect_now
Occurs while the server is starting up / not yet accepting connections, or in certain recovery states. Testable by racing connection attempts against a container that's mid-restart -- same "needs to own Postgres's lifecycle" infra gap as `08001`. Synthetic-injection only until then.

### Effectively unreachable in vulkan's current design -- synthetic injection only, and that's fine

#### RETRY-40001 -- serialization_failure
No call site uses `SERIALIZABLE` isolation (every `BeginTx` passes a bare `pgx.TxOptions{}` = READ COMMITTED) -- this code cannot fire today. Test: call `DatastoreRetry.Wrap` directly with a closure that returns a fake `*pgconn.PgError{Code: "40001"}` on its first call and nil on the second; assert `Wrap` retries and returns nil. Proves the mechanism now, in case isolation ever changes later.

#### RETRY-08007 -- transaction_resolution_unknown
#### RETRY-40003 -- statement_completion_unknown
Both are associated with two-phase commit / foreign-data-wrapper distributed-transaction scenarios in Postgres's own documentation -- vulkan is single-node and doesn't use 2PC or FDWs anywhere, so neither should ever fire in practice. Same synthetic-injection shape as `40001`: prove `Wrap` retries and recovers on the fake code, and leave it there. These two exist in the switch defensively, not because vulkan can trigger them.
