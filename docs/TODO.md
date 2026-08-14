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

- [x] Chunk 3 — controller declare verb (2026-08-13). The classify + switch
  driver landed in the datastore in chunk 2 (the decision must sit under the
  group lock); the controller layer is
  ConsumerController.DeclareBindings(ctx, groupId, patterns, declaredAt) ->
  (DeclarationOutcome, error): validation (groupId, no empty pattern,
  non-zero declaredAt), normalizePatterns (sort + dedup, non-nil so an
  empty set stores as {} not NULL), declarerIdentity() = hostname:pid. The
  DeclarationOutcome enum lives once in the pkg/consumer/binding vocabulary
  package (pkg/consumer is a door, so its vocabulary lives in subpackages —
  message precedent), imported by controller and datastore alike. Old
  Bind/ClearBindings untouched. Verified against dev DB:
  unsorted+duplicated input installs once, reordered same set joins, nil
  patterns = whole topic, three validation rejects.

- [x] Chunk 4 — consumer door: Register states the set (2026-08-13).
  Consumer.Register(ctx, group, topic, version, bindings) — nil = whole
  topic. Register attempts the declaration once (waiting is not an error,
  logged Info); the instance carries bindings + declaredAt + the consumer
  controller, and Consume re-attempts the declaration before starting the
  manager EVEN when Register installed — between Register and Consume the
  instance has no live heartbeat, so another declarer may have replaced the
  set. A waiting Consume retries every
  ConsumerConfig.BindingRetryInterval (default 10s) with a Warn per attempt
  (group, patterns, attempt, elapsed since declaredAt); cancelling ctx during
  the wait returns nil like any requested stop. Call sites rippled: alert
  executions (partitioncount, compactionreadcost) pass []string{JobName};
  cronlab's startConsumer takes the group's set; 8 whole-topic lab sites pass
  nil. Verified against dev DB (install/join at Register, waiting under a
  live incumbent, Consume waiting out the incumbent then swapping and
  consuming a routed message, clean stop); cronlab + alertlab pass.

- [x] Chunk 5 — delete the create-only path (2026-08-13).
  ConsumerController.Bind/ClearBindings and the datastore Bind/clearBindings
  pairs deleted (ListBindings + wildcardToRegex stay); alert declare.go
  drops its Bind call — the group is born undeclared and the consumer
  declares {JobName} at Register (no claim can run before then, so no
  whole-topic window). The wildcard semantics + forward-only doc moved from
  Bind onto ConsumerController.DeclareBindings; stale Bind mentions fixed in
  binding DDL comment and cron SlugPattern comment. Labs converted to
  DeclareBindings with full sets (cronlab/alertlab registerGroup helpers,
  topiclab, consumergrouplab, deletetopiclab; routinglab's reset declares
  nil = whole topic where it cleared bindings). replaceBindings' plain
  INSERT is now the only binding writer — the transitional duplicate-key
  window with Bind is gone. Sweep grep clean; routinglab, topiclab,
  consumergrouplab, deletetopiclab, cronlab, alertlab all pass.

- [x] Chunk 6 — read surface (2026-08-13). The per-pattern binding listing
  was REPLACED, not extended: binding rows are transactionally identical to
  the newest installed declaration, and the old listing could not show
  whole-topic sets or waiters, so ListBindings
  (admin/controller/datastore + Binding read-model + BindingData) was
  deleted and the surface is now MessageAdmin.ListDeclarations ->
  ConsumerController.ListDeclarations -> datastore
  ListBindingDeclarations. ONE private read serves listing and declare txn
  alike: listBindingDeclarations(ctx, querier, groupID) -- DISTINCT ON
  newest row per group/status/declarer with names, groupID 0 = every group
  (listWorkers widening-clause idiom); the txn calls it group-scoped on the
  tx and picks newestInstalledDeclaration in Go, one BindingDeclarationData
  struct. Controller composes in Go: effective =
  newest installed row per group; open waiter = a waiting row that is its
  declarer's newest row AND differs from the effective set (a waiter whose
  set someone else installed resolves silently -- joined appends nothing).
  Public read-model binding.Declaration (status reuses DeclarationOutcome).
  `vulkan alert bindings` keeps its name; columns now
  GROUP/TOPIC/VERSION/STATUS/PATTERNS/DECLARED BY/DECLARED AT/LAST ATTEMPT,
  empty set renders "(whole topic)". alertlab's exact-binding seeding check
  moved to the executor section (RegisterSystem no longer binds; only a
  running consumer declares) and asserts via ListDeclarations. Verified:
  10/10 scratch checks on dev DB (effective+waiter listing, swap, both
  waiter-resolution paths, whole-topic row), CLI renders live, alertlab
  passes.

- [ ] Chunk 7 — labs + gate.
  routinglab reshaped onto Register-declared sets; new lab covering same-set
  join, divergent-app wait, and dead-fleet swap/rolling-deploy convergence;
  grep labs for hand-copied binding SQL mirrors; full fresh-DB suite at
  review-ready; HISTORY entry, ROADMAP item slimmed to record pointer, this
  window cleared.
