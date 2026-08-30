---
status: accepted
date: 2026-08-30
phase: "pre-v1"
---

# 0624 — the first RegisterTopic stands up the control-plane schema

**Context.** Every program had to call `RegisterSystem` before its first `RegisterTopic`, yet for most of them the call carried nothing: `SystemConfig` has no fields, so the only content in `RegisterSystemConfig` is the two built-in alert schedules, which almost nobody sets. The quickstart and every playground carried a line whose only job was avoiding VK0017.

**Decision.** Public `RegisterTopic` checks for the system row and, when none exists, runs `RegisterSystem(ctx, nil)` before registering the topic. The check is register-if-absent, never a re-declare — a system registered with custom alert schedules keeps its declaration, because nothing runs when the row exists. `RegisterSystem` stays public as the path for setting cfg (and as `MigrateSystem`/`DestroySystem`'s sibling); every other verb keeps its VK0017 gate unchanged, including the internal `registerTopic` gate as a backstop. Topic catalog reads map 42P01 to absence the way `scanSystemData` already does — `GetTopic` returns `(nil, nil)` and `ListTopics` returns empty against an unregistered database, so the first error a misordered program sees is VK0005 ("register it with MessageAdmin.RegisterTopic first"), not a raw undefined-table error.

**Consequences.** The quickstart's samples and six playgrounds lose the `RegisterSystem` line; scenario 03 loses the whole admin detour. First boot is concurrency-safe without new locking: `system.register` already serializes table creation and the singleton seed under `pg_advisory_xact_lock`, and a probe racing six pools calling bare `RegisterTopic` on an empty database converged every round. The implicit register costs one `system_config` SELECT per RegisterTopic call.
