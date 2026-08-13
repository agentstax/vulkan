---
status: accepted
date: 2026-06-30
phase: "6.5b"
---

# 0163 — A lease covers (low, high], matching the claim read's convention

**Context.** The claim read selects messages with `id > low AND id <= high`.
The lease row records which range a worker owns; a reclaim must re-read
exactly what the original claim read.

**Decision.** The lease's `(low, high)` columns use the same half-open
convention as the claim read: low-exclusive, high-inclusive.

**Consequences.** A reclaimed range re-reads byte-identical to the original
claim — no off-by-one between what was claimed, what the lease says is owned,
and what a reclaimer reprocesses.
