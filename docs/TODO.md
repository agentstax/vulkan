# TODO

Sliding window of in-flight work only. Future work lives in ROADMAP.md;
shipped work in HISTORY.md; decision rationale in DECISIONS.md ->
docs/decisions/.

## Per-topic table split ([0571])

Split cursor, lease, key_lease, compaction_head, binding, binding_log into
per-topic interpolated tables (cursor_<id>, ...); shared schema reduces to
catalog + fleet + cross-scope history. Design settled in [0571]; this is
the build.

- [x] 1. internal/topic/tables.go: six new table-name funcs (CursorTable,
      LeaseTable, KeyLeaseTable, CompactionHeadTable, BindingTable,
      BindingLogTable).
- [x] 2. DDL move: delete the six CREATE TABLE blocks (plus the binding_log
      index) from system createSystemTables; add interpolated creates to
      topic createTopicTables. compaction_head_<id> drops the topic_id
      column, PK becomes compaction_key alone, shared-table comment goes.
      cursor/binding/binding_log keep their consumer_group FKs (target is
      shared catalog, created first). binding_log index name interpolated.
- [x] 3. Query sweep (~50 sites, ~20 files) -- interpolate names, drop
      topic_id predicates/columns from compaction_head queries; topicId is
      already threaded at every verified site:
      - consumergroup/controller/datastore: group.go (cursor insert, lease +
        key_lease deletes in deleteGroup), binding_log.go
      - consumergroup/messageconsumer/.../datastore: freshclaim, reclaim,
        commit, claim
      - consumergroup/deliveryconsumer/.../datastore: fanout
      - consumergroup/exceptionconsumer/.../datastore: claim, outcome
      - consumergroup/base/.../datastore: keylease
      - consumergroup/cursoradvancer/.../datastore: committed
      - producer/.../datastore: compaction, insert
      - compaction/.../datastore: head
      - metrics/.../datastore: consumergroup, topic
      - alert/compactionreadcost/.../datastore: compaction
      - cron/.../datastore: status
      - topic/janitor/.../datastore: keylease, sweep, drop
- [x] 4. Destroy paths: topic delete.go replaces its lease/key_lease/
      compaction_head DELETEs with DROP TABLE of the six new tables (drop
      list 4 -> 10 incl. partitions' parent). system delete.go: check how
      per-topic tables are dropped at system scope today and extend the
      same way.
- [x] 5. consumer_group_janitor sweep ([0573] amend): the one global
      binding_log DELETE becomes one batched DELETE per topic's
      binding_log_<id> per tick -- needs a topic-id list read from the
      catalog.
- [x] 6. Lab sweep: ~20 labs under examples/phase_1 carry raw SQL against
      the six tables (keyleaselab, compactionhead*lab, routinglab, ...) --
      update each to the interpolated names.
- [ ] 7. Verify: build + go test -race touched packages + directly-affected
      labs per chunk; full fresh-DB suite at review-ready. Close out:
      HISTORY.md entry citing [0571], slim the ROADMAP item away, empty
      this window.
