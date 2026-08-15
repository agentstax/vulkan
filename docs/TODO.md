# TODO

Sliding window of in-flight work only. Future work lives in ROADMAP.md;
shipped work in HISTORY.md; decision rationale in DECISIONS.md ->
docs/decisions/.

**Config layering + AlertRepeatInterval relocation** ([0515] [0516]) — chunk plan:

1. **Metadata layering machinery** (pkg/worker/controller): nested
   `{default, override}` value shape + generic resolve (effective = override
   ?? default); reshape janitor/manager/cronscheduler metadata structs +
   declarer seeds onto it; RegisterInstance resolves effective. Ripple: any
   lab reading flat poll_rate metadata.
2. **Consumer group tunables**: per-kind consumer metadata structs carrying
   ClaimPollRate, MaxRangeReclaims, ExceptionInitialBackoff, Message
   defaults, ConcurrencyOverride; consumer Register writes the default layer
   unconditionally; claim life resolves effective values; Message overrides
   clamp into code-owned MessageMin/MessageMax (warn on clamp).
3. **AlertRepeatInterval relocation**: repeat_interval into alert workers'
   metadata (declarer-seeded default); AlertController built from claimed
   metadata + live __system.alerts retention, clamp + warn; system table
   drops alert_repeat_interval_ns; SystemConfig/AlterSystem/`vulkan system
   alter` become field-less stubs.
4. **common.Update[T] + library reshape** ([0517]; reshapes the uncommitted
   chunk-4 code, library layer only): `common.Update[T]` (zero = unchanged,
   `Set(v)`, `Unset[T]()`); AlterGroupConfig fields onto it, Clear deleted;
   workercontroller.AlterWorkerConfig ClearOverrides -> UnsetOverrides;
   delete the system alter stub (AlterSystem, AlterSystemConfig,
   `vulkan system alter`).
5. **Group config CLI**: `group config get|set|unset` with the per-key
   table (parse/format/help — the single home for key knowledge), dotted
   `message.*` set (whole-doc forbidden), single-key get = filtered table;
   delete `group alter` + bare `group get`.
6. **Topic onto the surface**: AlterTopicConfig defaulted fields ->
   Update[T], admin resolves Unset from WithDefaults; `topic config`
   replaces `topic alter` (alter.go deleted); `topic get` drops the
   alterable columns.
7. **Cron onto the surface**: AlterCronJobConfig Timeout/Concurrency ->
   Update[T], Schedule/Data/Metadata stay set-only pointers; `cron config`
   replaces `cron alter`; `data.*` dotted set (JSON-literal-else-string
   inference), whole-doc set allowed, `unset data.<path>` deletes the
   field; schedule-reseed + register drift-gate docs move to the surviving
   verbs.
8. **Worker verbs + addressing**: `vulkan worker list / get / scale /
   suspend / resume` — poweruser door; addressing = worker name +
   --topic/--group scope flags, built here; scale writes target_instances
   through AlterWorker; admin Suspend/Unsuspend worker verbs; no
   register/destroy (rows owned by their domains).
9. **Worker config CLI**: `worker config get|set|unset` — reuses chunk-8
   addressing and the chunk-5 key-table machinery; reaches every row kind
   (consumer kinds, manager, waterline, janitor, cronscheduler, alert
   consumers incl. repeat_interval).
10. **Lab + close-out**: config-layering lab (override picked up next
    claim life, unset returns to default, alert repeat clamp); sweep labs
    for metadata mirrors; HISTORY/ROADMAP close-out; fresh-DB suite. Note:
    [0515] says "Register writes default" — the write actually happens in
    Declare at Consume start; fix wording in the HISTORY entry, not the
    record.
