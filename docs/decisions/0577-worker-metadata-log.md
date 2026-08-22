---
status: accepted
date: 2026-08-22
phase: pre-v1
---

# 0577 — Worker metadata history is an append-only worker_log

**Context.** [0570] settled the shape for config history — the current row
stays the enforced truth, a full-snapshot trail is appended in the same
transaction, machinery never reads the trail — and reserved the worker_log
name for worker metadata. The worker row has exactly one mutation site:
registerWorker's metadata-replace UPDATE (the newest declaration wins);
target_instances is written at INSERT only, and no suspend/alter verb
exists. Every process start redeclares every worker with unchanged
metadata, so the trail must not record restarts.

**Decision.**
- worker_log, a shared control-plane table beside worker: id BIGSERIAL PK,
  worker_id FK REFERENCES worker ON DELETE CASCADE, name, metadata,
  target_instances, declared_by, declared_at DEFAULT now(); index
  worker_log_worker (worker_id, id).
- name is denormalized into the snapshot for operator scans, even though
  workers never rename — topic_log needed it for renames, worker_log
  carries it for join-free reads.
- target_instances is in the snapshot though only creation sets it today:
  the snapshot is the full declared state, so a future suspend/alter verb
  appends to the same log with no schema change.
- Both write sites are inside registerWorker's existing transaction: the
  create path appends after the INSERT's RETURNING id; the replace path
  appends only when the UPDATE's RETURNING says the metadata changed
  (stored.metadata IS DISTINCT FROM w.metadata). A no-change redeclare
  writes no log row — log volume tracks config change, never restarts.
- declared_by = common.ProcessIdentity, passed by the controller as
  topic's Register does; RegisterWorker gains a declaredBy param.
- Machinery never reads worker_log; it exists for operators. No retention
  — rows append only on actual change; a TTL revisit for both worker_log
  and topic_log is parked in ROADMAP.

**Consequences.** Worker config history becomes queryable — when a value
changed and which process declared it — with no second read path and no
new transaction. The replace path's UPDATE still runs on no-change
redeclares (one dead tuple per worker row per process start; negligible,
per-start not per-tick) — gating it needs a follow-up read to keep
ErrDeclarationInterrupted honest, left out of scope. Completes [0570]'s
reservation; worker_run_log (failure evidence, parked) is unrelated.
