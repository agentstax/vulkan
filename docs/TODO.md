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
4. **AlterGroup + group CLI**: admin.AlterGroup + AlterGroupConfig (sparse
   pointers + explicit clear signals) writing the override layer;
   `vulkan group alter` (--clear) and `vulkan group get` (effective value +
   source per tunable).
5. **Worker CLI tree**: `vulkan worker list / get / alter / suspend /
   resume` — poweruser door; alter writes overrides + target_instances; no
   register/destroy (rows owned by their domains).
6. **Lab + close-out**: config-layering lab (override picked up next claim
   life, clear returns to default, alert repeat clamp); sweep labs for
   metadata mirrors; HISTORY/ROADMAP close-out; fresh-DB suite.
