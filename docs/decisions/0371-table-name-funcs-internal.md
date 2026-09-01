---
status: superseded by [0628]
date: 2026-07-28
phase: "13"
---

# 0371 — The table-name functions move to internal/topic

**Context.** `topic.LogTable`/`PartitionTable`/`DeliveryTable`/`DeliveryLogTable` were exported functions any caller could reach, but they are cross-package plumbing: zero example programs call them, and every real call site is `pkg/producer`/`pkg/consumer`/`pkg/consumer/metrics` building raw SQL against tables `pkg/topic` owns.

**Decision.** All four moved to a new `internal/topic` package — module-internal, so `producer`/`consumer`/`consumer/metrics`/`topic` can still import it, but nothing outside the module can. Unqualified `topic` import in the three consumer packages; aliased `iTopic` inside `pkg/topic/datastore.go`, which already has local `topic *Topic` variables that would shadow the import.

**Consequences.** They are gone from `pkg/topic`'s public API, enforced by the compiler rather than doc-comment intent. **Rejected:** plain unexport — three different packages call them, so lowercasing breaks cross-package compilation. **Rejected:** `*Topic` methods — every real call site only carries a bare `topicID int64` (the datastore interface deliberately takes `topicID`/`partitionSize`, not `*topic.Topic`), so methods would force threading `*Topic` through the whole interface for zero user-facing benefit.

**Superseded by [0628]** — the funcs are public API on `pkg/topic`.
