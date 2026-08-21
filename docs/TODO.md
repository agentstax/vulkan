# TODO

Sliding window of in-flight work only. Future work lives in ROADMAP.md;
shipped work in HISTORY.md; decision rationale in DECISIONS.md ->
docs/decisions/.

## Stop line as session summary ([0567])

- [x] **Chunk 1 -- counters live on the metrics producer.** DONE
  2026-08-21: metrics.SessionCounters in consumergroup.go; MetricsProducer
  atomic fields + Record* methods + Snapshot(); Add/Remove renamed
  RecordAbandoned/RecordCleared (RecordAbandoned bumps the monotonic
  counter); abandonedEvents -> metrics across base/provisioners/instance.
  Build + race tests + conventions + abandoned-routine-snapshot-lab pass.
  metrics.SessionCounters read-model in pkg/metrics (Claimed, Success,
  Superseded, Ready, Deferred, Dead, Reclaimed, Quarantined, Abandoned,
  LeaseLost); MetricsProducer gains atomic counter fields, Record*
  bump methods, and a Snapshot() adapter; Add/Remove bump Abandoned
  internally. Rename the abandonedEvents fields/params across the
  threading route (consumer instance, base provisioner/consumer, worker
  provisioners) -- the object is the instance's metrics side-channel
  now, the type name MetricsProducer stays.
- [x] **Chunk 2 -- bump sites in the worker runners.** DONE 2026-08-21:
  BaseConsumer.Metrics exported; messageconsumer prefetch bumps
  reclaimed (Lease.Reclaims > 0) / quarantined (new Quarantined marker
  on ClaimedRangeData/ClaimedRange -- quarantine now returns the marker
  instead of falling through to fresh claim) / claimed, runItem bumps
  success+superseded at resolution, countDeliveryRows counts
  ready/deferred/dead on landed Commit/PartialCommit, lease_lost on
  both ErrLeaseLost branches. exceptionconsumer: Kill returns its dead
  count (runner bumps), attempts-exhausted escalation MOVED from
  datastore.recordFailure to the runner (policy at the driver;
  datastore keeps dumb verbs), record* methods bump
  success/superseded (resolution) and ready/dead (landed),
  absorbLostLease bumps lease_lost. deliveryconsumer (standalone
  lifecycle-path sub-consumer, not in the instance chain) deliberately
  not instrumented. Build + conventions + reclaim-lab + defer-lab pass.
- [x] **Chunk 3 -- stopped line renders the snapshot.** DONE 2026-08-21:
  Consume emits logStopped on every exit (clean-stop gate dropped;
  error still returned, never logged), identity + `duration` + all ten
  `<verb>_count` attrs, zeros printed. CONVENTIONS start-line section
  amended (session summary, every exit, memory only). Verified live:
  claimed_count=3 success_count=3 + eight zeros on a real session.
  OPEN for review: standalone worker instances' stopped lines (janitor,
  cron scheduler, collector, cursor advancer) -- own local counters,
  this build or ROADMAP follow-on; amendment phrased "every lifetime
  counter the instance keeps," so counter-less lines comply meanwhile.
- [x] **Chunk 4 -- the line carries its own breadcrumb.** DONE
  2026-08-21, record [0568]: Declare the
  stopped line: unexported event in pkg/consumer/logs.go (VK0038
  precedent), next VK serial, message "consumer stopped", empty
  consequence; logStopped logs code first and a trailing
  `help` attr -- plain words ending in the pasteable command
  ("counters explained: vulkan explain VK00xx"). Hand-written docs
  page defining every counter (what it counts, what nonzero means,
  paired gauge) lands in the same change -- this IS the session-summary
  page. CONVENTIONS: `help` attr-registry row + Declared-events
  boundary widened one notch (a lifecycle summary line whose attr set
  needs a docs page may declare, Info included) + session-summary
  paragraph mentions code/help. Decision record extending [0562].
  Parking lot: evaluate pipeline-enrich auto-help for ALL coded lines.
- [ ] **Chunk 5 -- flush tick to __system.metrics + metric
  declarations.** vulkan.consumer.session.* declared as first-class
  registry entries in pkg/metrics (name, kind, unit, description --
  const comments become descriptions under the "what nonzero means"
  grammar); flusher builds Measurements FROM the declaration (kind/
  unit can't drift); session uuid minted per Consume as series attr;
  tick in MetricsProducer.Run produces totals as KindCounter.
  otelvulkan passes WithDescription for names the registry knows ->
  Prometheus # HELP / Grafana hover. Decide tick interval + last-flush
  (lean no: stop line holds final numbers).
  - Descriptions state the flow-vs-level split: session.* never
    reconcile against consumer.cursor.*/exceptions.* gauges.
  - Consider batching abandoned-routine event produces onto the tick
    (ProduceBatch) -- today's path is already storm-bounded (256-cap
    queue, drop-on-full, one produce in flight); events' At stamps at
    enqueue so latency math survives batching, only freshness lags.
- [ ] **Chunk 6 -- vulkan explain gains a metrics section.** List
  metric declarations beside errors and events; explain by metric name
  or stop-line attr key (ready_count -> vulkan.consumer.session.ready).
- [ ] **Chunk 7 -- checkpoint.** Fresh-DB run of affected labs + full
  suite; HISTORY.md entry citing [0567]+[0568]; drop the ROADMAP Now
  item.
