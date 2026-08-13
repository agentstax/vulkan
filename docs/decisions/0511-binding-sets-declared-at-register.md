---
status: accepted
date: 2026-08-13
phase: "14a"
---

# 0511 — Binding sets are declared at consumer Register; replacement waits for zero live declarers

**Context.** Bindings were create-only: `Bind` inserted with ON CONFLICT DO
NOTHING and nothing called `ClearBindings`, so a changed pattern on the same
group silently matched the union of old and new sets. Nothing recorded who
declared a group's set or that a registration was blocked trying to change it.

**Decision.** `Register(ctx, group, topic, version, bindings)` states the
group's full set; no bindings = whole topic. Bind and ClearBindings leave the
public surface. Per registration:

- Same set as stored, or no declaration stored: install/join immediately.
- Different set while a live instance (fresh worker_instance heartbeat on the
  group's workers) still declares the stored set: the registration waits —
  visibly, retried on an interval, logged loudly, forever. No timeout knob;
  deadline policy belongs to orchestrator readiness probes. A running
  incumbent is never fenced or killed.
- Different set with zero live declarers: swap the set in one transaction and
  join.

The wait lives in Consume: Register attempts the install once and returns;
Consume retries on an interval before starting the manager, so a waiting
instance never consumes and never goes ready.

Storage is an append-only `binding_declaration` ledger, one row per Register
attempt: `installed` rows are set changes (the effective declaration is the
group's newest installed row, and set-change history is retained as audit);
`waiting` rows are attempts blocked on a live incumbent's different set,
re-appended on every retry (declarer identity hostname:pid, display only;
the requested set travels as a `patterns TEXT[]` column). Each row carries
two timestamps: `declared_at`, when the declarer first stated the set —
constant across its retries — and `attempt_at`, when this attempt ran; a
declarer's newest attempt_at is its liveness heartbeat, and the installed
row ending a wait records the whole wait span on its own. No row is ever
updated — the write path only inserts; retention of superseded attempt rows
is a deferred cleanup pass (ROADMAP), the same append-then-prune shape as
message_log, and self-contained rows are what let pruning eat old retries
without losing when a wait began.
Declaration-level rows rather than per-pattern rows because an empty
(whole-topic) request must be able to wait. `status` is typed and validated
in Go, not CHECK-constrained. With no uniqueness on installed rows,
concurrent installers serialize on the consumer_group row lock inside the
swap transaction. Claims keep reading `binding` rows, which stay mutable and
remain the effective set's only home — a newest-generation filter there
would tax the claim hot path; after this ships only the install/swap
transaction writes them.

**Consequences.** A rolling deploy converges when the old fleet's heartbeats
lapse — the deploy killing the old instances is the consent; rollback is
symmetric, so durable state never outlives running code. Two divergent apps
on one group: first to register wins untouched, the second waits forever and
never goes ready. No union window: each claim reads exactly one declared set.
Waiting rows are informational; a dead waiter self-describes through the age
of its newest waiting row.

**Rejected.** Additive Bind plus a SetBindings convergence verb
(last-writer-wins flip-flop under divergent apps); versioned declarations (a
durable version cannot see a rollback, so a rolled-back fleet silently runs
under new bindings); union of live instances' patterns (divergent apps run as
the union forever); epoch fencing that kills mismatched incumbents (divergent
apps crash-loop each other).
