---
status: accepted
date: 2026-06-20
phase: "3.5"
---

# 0081 — Measure synchronous_commit only; skip the batch-ack measurement

**Context.** The throughput wall is the per-message success/failure path: one single-row `UPDATE … WHERE id=$1 AND lease_token=$2` via autocommit `Pool.Exec`, so every message pays its own commit and WAL fsync. The claim commits once per batch, so it already amortizes. An upcoming topology change (append-only log with cursors) will reshape the table layout.

**Decision.** Measure only `synchronous_commit` — whether `COMMIT` blocks on the fsync or returns early and lets the WAL writer flush asynchronously. It is the one portable lever: a global durability knob, blind to table layout, so the result survives the topology change.

**Consequences.** No commit-batching code is built. **Rejected:** measuring batch-ack — it was measure-only anyway, and the cursor model is its limit case (N messages recorded by one integer position write), so the bridge is understood without building it.
