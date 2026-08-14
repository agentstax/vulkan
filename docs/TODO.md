# TODO

Sliding window of in-flight work only. Future work lives in ROADMAP.md;
shipped work in HISTORY.md; decision rationale in DECISIONS.md ->
docs/decisions/.

## Binding lifecycle (ROADMAP Now, picked up 2026-08-13)

Design settled 2026-08-13 (full notes + rejected alternatives in the ROADMAP
entry): a group's bindings are declared as a full set at consumer Register;
the stored set is replaceable only when no live instance (fresh
worker_instance heartbeat on the group's workers) still declares it; a
mismatched registration waits visibly, forever, never fencing an incumbent.

- [x] Chunk 1 — decision record + declaration storage DDL (2026-08-13).
  Record 0511 written. Settled: the wait runs in Consume (Register attempts
  the install once and returns; Consume retries on an interval before
  starting the manager); lifecycle state is an append-only
  binding_declaration ledger, one row per Register attempt — installed rows
  are set changes (effective = newest installed row per group, history kept
  as audit), waiting rows re-appended each retry; per row declared_at =
  episode start (constant across retries, supplied by the controller) and
  attempt_at = this attempt, newest per declarer doubling as its liveness
  heartbeat; no row is ever updated;
  retention of superseded attempt rows deferred to ROADMAP (append-then-
  prune, like message_log); status typed in Go, no SQL CHECK; claims keep
  reading binding rows, unchanged. dev-DB drop+recreate verified, bootstrap
  idempotent.

- [x] Chunk 2 — datastore verbs (2026-08-13; add-only, Bind/ClearBindings
  untouched). consumercontroller/datastore/binding_declaration.go:
  DeclareBindings(ctx, groupID, patterns, declaredBy, declaredAt) ->
  (DeclarationOutcome installed|joined|waiting, stored patterns, error) —
  one transaction: consumer_group row FOR UPDATE (serializes installers),
  newest installed declaration read, element-wise set compare (patterns
  arrive sorted+deduplicated from the controller), inline fresh
  worker_instance EXISTS, then append installed + swap binding rows /
  append waiting row / join with nothing written. One flat read for both
  the transaction and callers: ListDeclarations(groupID, status) — newest
  attempt row per declarer via DISTINCT ON, served by the
  (consumer_group_id, status, declared_by, id) index; effective set =
  newestDeclaration (highest id) over the installed rows, open waiters =
  the caller's comparison against it. BindingDeclarationData + status
  consts in model.go. Verified against dev DB: install/join/swap, blocked +
  retry appends, dead-fleet install keeping the wait span, empty-set
  install.

- [ ] Chunk 3 — controller classify + declare verb.
  Pure classify(declared set, stored set, live declarers) -> named-action
  enum (join / swap / wait) + exhaustive switch driver; a controller verb the
  consumer door calls per attempt, all input validation here (pattern
  validation moves from Bind). Old Bind/ClearBindings still present so the
  build stays green.

- [ ] Chunk 4 — consumer door: Register states the set.
  Consumer.Register(ctx, group, topic, version, bindings) — no bindings =
  whole topic. Register attempts install once; Consume retries on an
  interval with loud logs until installed, then starts the manager. Every
  Register call site ripples with its true set in the same chunk: alert
  executions (partitioncount, compactionreadcost) pass JobName so their
  binding never regresses to whole-topic mid-migration.

- [ ] Chunk 5 — delete the create-only path.
  Alert declare.go Bind calls, ConsumerController.Bind/ClearBindings, and
  the datastore Bind/clearBindings pairs all go; grep for stragglers.
  Register is now the only writer of binding rows.

- [ ] Chunk 6 — read surface.
  ListBindings (datastore -> controller Binding read-model -> MessageAdmin)
  gains declarer + waiting-declaration info; `vulkan alert bindings` columns
  extended to show them.

- [ ] Chunk 7 — labs + gate.
  routinglab reshaped onto Register-declared sets; new lab covering same-set
  join, divergent-app wait, and dead-fleet swap/rolling-deploy convergence;
  grep labs for hand-copied binding SQL mirrors; full fresh-DB suite at
  review-ready; HISTORY entry, ROADMAP item slimmed to record pointer, this
  window cleared.
