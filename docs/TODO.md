# TODO

Sliding window of in-flight work only. Future work lives in ROADMAP.md;
shipped work in HISTORY.md; decision rationale in DECISIONS.md ->
docs/decisions/.

## Documentation-driven pass (picked up 2026-08-22)

Settled: docs drive implementation (page = the proposal, reviewed before
code); all pages rewrite to the REAL API; vocabulary per CONVENTIONS.md
## Vocabulary (one registry for code, comments, and docs); no performance
number without a benchmark record.

Site triage 2026-08-22 — all 19 non-error pages read, verdicts:

- **Already real**: guides/migrations (sidebar fix DONE 2026-08-22);
  errors/* untouched.
- **Rewrite to the real API**:
  - quickstart — DONE 2026-08-22: rewritten against the real API (samples
    compile-checked in a scratch module against the working tree; site
    build green). Real path shown honestly: migrate init CLI, 8-step
    producer / 7-step consumer programs, psql inspection with real
    per-topic table names, ProduceFunc as the transactional headline,
    manager-run upkeep aside. No perf numbers; Postgres claim = "test
    suite runs against 17" (the old ">=14" was unsourced).
  - Startup friction observed while writing (feeds
    DefaultProducer/DefaultConsumer, ROADMAP Next): consumer needs a
    MessageAdmin + RegisterSystem just to GetTopic; topic.SchemaVersion(1)
    literal repeated 3x per program; Consume's cancellable-ctx requirement
    is a context.Background() trap; ConsumerConfig.Retry vs
    Message.Retry confusable; produce-only deployments silently get no
    upkeep without `vulkan manager run`; RegisterTopic wants
    &topiccontroller.TopicConfig{} (an import + empty struct for the
    common case; nil-ability unverified); pkg/common and pkg/topic names
    invite aliasing in user code.
  - ALL REWRITES DONE 2026-08-22. Every non-error page except cloud now
    documents the real API; every Go sample compile-checked in the
    scratch module (txguide/, routing/, pagescheck/); site build green
    (74 pages); vocabulary sweep run over all rewritten pages.
  - guides/transactional-enqueue — renamed guides/transactional-produce
    (banned verb in title + slug; 5 inbound links + sidebar updated).
  - concepts/streams — renamed concepts/fan-out ("stream" banned), title
    "Fan-out, Retention & Replay". Real: fan-out, retention
    (RetentionTTL, AllowDropPastCommitted default-blocks drops — the
    retention cliff is defused by default), new-group-reads-history.
    Rewind of an existing group -> proposed.
  - concepts/lifecycle — rewritten on the real cursor-path model:
    success writes no per-message row; delivery rows materialize on
    failure only (ready/inflight/deferred/dead); range leases + reclaim
    + quarantine; kill backstop; redrive -> proposed aside.
  - concepts/ordering — retitled "Ordering & Concurrency". Real story:
    id-order claims, no completion-order guarantee, per-key exclusivity
    = compaction key + ConcurrencyDefer (latest-wins, NOT FIFO); strict
    per-key FIFO -> proposed. OrderBestEffort/partition keys deleted.
  - concepts/queue-and-log — fusion thesis kept, restated on the real
    mechanism (cursor for success, delivery rows for failure). The old
    "choose cursor vs lifecycle per stream" pitch is GONE — it would
    re-open the ConsumerType demotion; re-litigating that is a user
    decision, deliberately not taken here.
  - concepts/routing — real binding model: []string at Register (whole
    set, nil = whole topic), `*` = any-run-of-characters wildcard
    (depth-crossing, no NATS `>`), installed/joined/waiting outcomes,
    forward-only changes, header matching -> proposed. Old retroactive-
    routing pitch removed (depends on unshipped rewind).
  - concepts/architecture — real control-plane + per-topic families, the
    cursor/delivery flow, poll-only claims (LISTEN/NOTIFY absent ->
    proposed), worker-fleet table, no performance numbers.
  - site roadmap page — Built section now lists the shipped engine
    (log/queue split, routing, retention, compaction, fleet, VK codes,
    compat gate); Now = benchmarks + docs; Later = demand-driven list
    from docs/ROADMAP.md.
  - index + why-vulkan — real hero samples (ProduceFunc + Consume),
    honest steps (new-group-history instead of FromOffset), cards
    rescored, SQL on real tables, cloud labeled "planned", numbers
    stripped.
  - guides/dead-letters — real triage SQL (delivery/message_log/
    delivery_log joins), metrics-based watch step, redrive -> proposed
    with spec sketch.
  - guides/replay — split: new-group bootstrap = shipped; rewind of an
    existing group = proposal spec with open questions recorded.
  - demo — marked Proposed (command does not ship); reworded onto the
    real schema (committed-cursor scoreboard) and the existing
    failure-injection labs it would package.
  - compare/* — rescored to shipped behavior, proposed items labeled
    never checkmarked; all unbacked numbers stripped (tens-of-thousands
    msg/s, ~50k graduation) pointing at the benchmark pipeline instead.
  - Status markers DONE: sidebar badges (demo "Proposed", replay
    "Partly proposed"); in-page Asides mark proposed sections on mixed
    pages.
- **Remaining, needs user input**:
  - cloud page — parked user call: unlink from sidebar or mark as
    vision (index/why-vulkan now say "planned" when linking to it).
  - site deploy (`just site-deploy`) — never run without asking.
  - queue-and-log: whether to re-open the per-topic
    cursor-vs-lifecycle-choice pitch (ConsumerType demotion) — the
    rewrite documents the unified reality instead.
- **Docs mechanism brainstorm (2026-08-22, in progress)** — invented
  interactive machinery for the site; user verdicts on round one:
  - SPIKE PROMISED (user: "don't let me forget"): PGlite feasibility —
    run the baseline topic-family DDL unmodified in PGlite, execute the
    real produce insert, ~30 min. Gates the live-database-under-every-
    page idea (tier 1 = replay the real SQL literals against PGlite,
    drift-checked via `-- vulkan:` owner tags; tier 2 = Go wasm +
    pgconn DialFunc bridge to execProtocolRaw, one flagship page only).
  - COMMITTED by user: paste-your-log-line error/event pages (parse
    attrs, interpolate the reader's values into fix commands); compat
    verdict widget (pick build + target version, compute the real
    MinCompatibleVersion gate answer).
  - HESITANT: retry-curve slider playground — user finds it not unique
    (any queue's docs could ship it).
  - EXPLORE, details matter: quickstart as verifiable psql transcript —
    not sold yet; likely depends on the PGlite spike.
- **Code findings from the docs pass**:
  - FIXED 2026-08-22: RoutingKey doc comment (options.go) now states
    the real reach — a keyless message matches no binding, only
    unbound groups receive it (matches the SQL + routinglab).
  - FIXED 2026-08-22: worker_log added to deleteSystemTables' reverse-
    order drop list and to destroysystemlab's assertion list;
    destroy-system-lab green (drop + re-register both asserted).
  - Friction (feeds API ergonomics): ProduceInTx accepts only a
    ProducerFunc — a static payload in a caller-owned transaction costs
    an inline 3-arg closure per topic; no value-taking form. Also: no
    "start from now" option for a new consumer group — deep-retention
    topics force full history reads on every new group.
