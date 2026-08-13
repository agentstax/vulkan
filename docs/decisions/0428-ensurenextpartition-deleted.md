---
status: accepted
date: 2026-08-11
phase: "14a"
---

# 0428 — EnsureNextPartition deleted without a replacement home

**Context.** When pkg/maintain was deleted, its proactive partition create-ahead duty (EnsureNextPartition) needed either a new home among the workers or deletion.

**Decision.** Deleted outright, with no rehome. Partition creation is provisioning and belongs to the write path — partition 0 exists from RegisterTopic, and the produce path's reactive ensureCoveringPartition creates every later partition. The janitor is cleanup only.

**Consequences.** Correctness rests entirely on the reactive path; no background worker pre-creates partitions. If proactive create-ahead ever returns, it comes back as a best-effort producer-side trigger, never as a correctness layer. **Rejected:** rehoming the duty onto a worker — it would keep a second creation path alive beside the one that already had to be correct on its own.
