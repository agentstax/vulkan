---
status: accepted
date: 2026-08-12
phase: "14a"
---

# 0465 — Job-request status is derived from existing tables, never written

**Context.** Operators need to know what happened to each produced job request per consumer group — pending, succeeded, failed, or superseded. That could be a status column with writes on every transition, or a computation over records the system already keeps.

**Decision.** There is no status column and there are no status writes. Status is computed from three existing sources: `message_log` (every produced request, compaction keeping losers physically present within retention), `compaction_head` (which request is current per job), and `delivery_log` (what each group did with each request). "Superseded" is computed as "this group never ran it and it is no longer the head", and the superseding request is simply the next-newer message on the key.

**Consequences.** Nothing can drift: the status view is always consistent with the records that produced it, and the same three sources answer both the per-group rollup and the per-request listing with `SupersededBy`/`SupersededAt`. **Rejected:** writing a `delivery_log` row when a pending request is superseded — write amplification on compacted streams for a fact `message_log` + `compaction_head` already record; one mechanism per fact.
