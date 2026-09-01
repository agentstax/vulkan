---
status: accepted
date: 2026-08-31
phase: "pre-v1"
---

# 0626 — newest-wins is the only declaration form

**Context.** [0625] carried two strict forms beside newest-wins for a stored group config: `ConsumerConfig.RequireMatch`, refusing a differing stored document with `ErrGroupConfigMismatch` (chunk 12), and a stale-build gate refusing a declaration whose document would drop fields a newer build declared, on the migration gate's `min_compatible_version` mechanics (chunk 13). Chunk 12 was built and reverted in full; chunk 13 was cut before code.

**Decision.** A differing declaration always overwrites, and the overwrite is a declared Warn naming the change: VK0059 for a worker row (the group config), VK0061 for a topic, VK0062 for a schedule — the latter two promoted from undeclared Info lines. Each consequence clause says what a repeat means: two services declare the resource with different configs and overwrite each other. One declaring service per resource is a convention the log makes visible; the library enforces nothing. Both strict forms are rejected as a lock with no key. `RequireMatch`: the service that sets it can never have its config changed without redeploying it as accepting first, deploying the change, then setting it back. The stale-build gate: a rollback becomes an outage — the older build's `RegisterConsumer` is refused and no verb lowers the stored floor. The gate also has no premise on the migration gate's own terms: a new config field is additive (older builds run without it, an absent field resolves to the default), which is `MinCompatibleVersion = 0`, the floor that never locks anyone out; the sparse `omitempty` document cannot tell an older build from an unset field without a version stamp and a hand-kept field->version registry; and a dropped field is source code, back the moment the newer build declares again. The `Declaration` outcome (created / joined / updated) is parked separately in ROADMAP Later.

**Consequences.** [0625]'s `RequireMatch`/`ErrGroupConfigMismatch` and stale-build clauses are superseded by this record; the rest of [0625] stands. guides/consumer-group-config.mdx and guides/client.mdx lose their strict-form sections and say so; VK0061/VK0062 pages land with their declarations; `schedule_config` keeps no declaration trail, so VK0062's diagnose query shows the current row only (the trail is a ROADMAP item). Both strict forms sit in the ROADMAP parking lot; reviving either needs a concrete workload the warn did not serve, and a verb that unlocks it.
