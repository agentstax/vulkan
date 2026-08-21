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
- [ ] **Chunk 3 -- stopped line renders the snapshot.** Consume emits
  "consumer stopped" on EVERY exit (drop the clean-stop gate), with
  identity + `duration` (session wall time) + every counter as
  `<verb>_count` attrs, zeros printed. CONVENTIONS ### The start line
  amendment: the stopped line is the session summary. Decide here
  whether the standalone worker instances' stopped lines (janitor,
  cron scheduler, collector, cursor advancer -- trivially local
  counters) ride this build or go back on the ROADMAP.
- [ ] **Chunk 4 -- flush tick to __system.metrics.** Session uuid
  minted per Consume as a series attribute; vulkan.consumer.session.*
  name consts in pkg/metrics; a tick in MetricsProducer.Run produces
  current totals as KindCounter Measurements. No otelvulkan change
  (KindCounter already maps to the monotonic observable). Decide the
  tick interval + whether shutdown attempts a last flush (lean no:
  queued events already drop on cancel; the stop line holds the final
  numbers).
  - Name-const comments must state the flow-vs-level split: session.*
    are per-instance monotonic flows and never reconcile against the
    consumer.cursor.*/exceptions.* gauges (levels) -- kept-both
    evaluation noted on the ROADMAP item.
  - Consider moving abandoned-routine event produces onto the flush
    tick too (batch the queued events per tick, e.g. ProduceBatch)
    instead of one produce per event -- an abandoned-routine storm
    shouldn't add per-event DB writes. Weigh against: today's path is
    already storm-bounded (256-cap queue, drop-on-full, one produce in
    flight), and events are paired add/clear rows whose snapshot
    latency matching may care about produce timing.
- [ ] **Chunk 5 -- checkpoint.** Fresh-DB run of affected labs + full
  suite; HISTORY.md entry citing [0567]; drop the ROADMAP Now item.
