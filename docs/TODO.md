# TODO

Sliding window of in-flight work only. Future work lives in ROADMAP.md;
shipped work in HISTORY.md; decision rationale in DECISIONS.md ->
docs/decisions/.

## binding_log retention

Design settled 2026-08-21 (ROADMAP Now item). Sweeps the shared binding_log
table today; the per-topic split ([0571]) amends the sweep to one DELETE per
topic table when that work ships.

- [x] Chunk 1: pkg/consumergroup/janitor/controller (+ datastore) --
      SweepExpiredWaitingDeclarations: one batched DELETE of waiting rows past
      the TTL, keeping each declarer's newest waiting row; installed rows
      never touched. Each domain's cleanup worker is its janitor: the topic
      kind renamed "janitor" -> "topic_janitor" (WorkerTopicJanitor), this
      one is "consumer_group_janitor"; SQL owners topicjanitor./
      consumergroupjanitor.
- [ ] Chunk 2: worker package (provisioner/config/metadata/instance) on the
      metrics-collector template -- worker kind consumer_group_janitor,
      OwnerSystem (one worker row total), poll rate on the order of hours,
      flat 7d TTL const, Debug swept_count line only on ticks that deleted
      rows.
- [ ] Chunk 3: wiring (admin system declarers, systemmanager + consumer
      embedded-manager provisioner lists), lab coverage, decision record +
      HISTORY entry + ROADMAP slim, TTL const onto the hardcoded-config
      audit.
