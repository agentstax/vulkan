---
status: accepted
date: 2026-07-20
phase: "11.5"
---

# 0346 — Migration registries are explicit ordered slices, not init() registration

**Context.** The runner needs the full ordered set of system-scope and topic-scope migrations; a common Go pattern is side-effect registration in `init()` from each migration's own file. Dated approximately; built across July 2026.

**Decision.** `pkg/system/migrations` and `pkg/topic/migrations` each export an explicit ordered slice, backed by a contiguity unit test — no `init()` registration.

**Consequences.** The complete migration sequence is readable in one place and reviewable in a diff; the contiguity test catches a skipped or duplicated version number at test time. **Rejected:** `init()` registration — order and completeness become emergent properties of which files got imported rather than a declared list.
