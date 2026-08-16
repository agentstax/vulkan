# TODO

Sliding window of in-flight work only. Future work lives in ROADMAP.md;
shipped work in HISTORY.md; decision rationale in DECISIONS.md ->
docs/decisions/.

## Multi-message Produce ([0525])

- [x] 1. ProduceItem + ProduceBatch (pkg/producer). ProduceItem{Message,
  Options} with NewProduceItem rejecting a nil message and a set
  IdempotencyKey (caller keys stay single-Produce-only). ProduceBatch on
  ProducerInstance: at least 1 item, per-item Message-options Fill +
  Validate, fresh v7 minted per item, adapt to controller.NewAppend, one
  controller.AppendMessageBatch call with cfg.BatchAttemptTimeout --
  all-or-nothing, results in argument order, failedIdx joined into the
  error when >= 0. Never touches pkg/producer/batcher. As built: as
  planned; produce_item.go holds the pair type, ProduceBatch sits between
  Produce and ProduceFunc in producer_instance.go.
- [x] 2. Dogfood. collectConsumerGroup and both alert produceCheckSummary
  methods replace their errgroup fan-outs with one ProduceBatch call; the
  "concurrent sends share the producer's batched transactions" comments go
  with them. As built: as planned; the alert executions dropped their
  errgroup imports.
- [x] 3. Checkpoint. producer-batch-lab (or a section in it) proves a
  ProduceBatch lands as ONE transaction (xmin grouping, the lab's existing
  technique), ids in argument order, and the caller-key rejection;
  alert-lab + metrics-collector-lab re-run on the new path; full fresh-DB
  suite. As built: new produceBatchScenario -- 30 items, single xmin, ids
  ascending in argument order, a jsonb-poisoned item rolls the whole batch
  back with "item 2" named in the error, caller-key and empty-batch
  rejections; alert-lab + metrics-collector-lab green on the new path;
  fresh-DB suite 36/36.
