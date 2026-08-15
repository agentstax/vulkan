---
status: accepted
date: 2026-08-15
phase: "14"
---

# 0516 — AlertRepeatInterval moves to alert worker metadata; system config becomes a stub

**Context.** AlertRepeatInterval was the system row's only config column.
Each alert execution re-read it per claimed life, and NewAlertController
validated it against alert.TopicConfig()'s registration-default retention --
so an operator lowering __system.alerts retention below the repeat interval
silently broke the active-head-republishes-before-sweep guarantee.

**Decision.**
- repeat_interval moves into each alert consumer's worker-row metadata, in
  [0515]'s {default, override} shape -- per-alert repeat, tunable via
  `vulkan worker alter` (a dedicated alert alter can come later if it earns
  it). The declarer seeds the default.
- The claim-time build of AlertController swaps its system-row read for its
  own claimed metadata plus a LIVE __system.alerts topic-row read. A repeat
  at or above live retention is clamped below it with a warning -- alerts
  keep flowing rather than stopping over a config mistake, closing the
  stale-retention hole at every claim.
- The system table drops alert_repeat_interval_ns (baseline DDL edit). The
  system config surface (RegisterSystem cfg / AlterSystem /
  `vulkan system alter`) stays as a field-less stub for future system-wide
  knobs; whether it survives into the v1 public surface is decided in 14b.

**Consequences.** The system row is a bare singleton anchor;
ErrSystemConfigMismatch has nothing to compare until a future knob lands.
CLI system get/alter shrink to the stub. The repeat-vs-retention invariant
is now enforced against live state instead of a registration default.
