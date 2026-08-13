---
status: accepted
date: 2026-07-11
phase: "8c"
---

# 0265 — Optional produce inputs travel in a ProduceOptions struct, not positional strings

**Context.** Compaction added a second optional per-message input (`CompactionKey`) alongside the existing routing key, and positional optional strings do not scale or self-document at call sites.

**Decision.** `ProduceOptions{RoutingKey, CompactionKey string}` — a sparse struct threaded through `Produce` → `AppendMessage` → `appendMessage`'s INSERT — matching the codebase's existing `*Config` convention for optional parameters.

**Consequences.** Future optional produce inputs extend the struct without breaking call sites; zero-value fields mean "not set" (`NULL` `compaction_key` = not compacted). **Rejected:** positional string parameters — ambiguous at call sites and breaking on every addition.
