---
status: accepted
date: 2026-08-19
phase: 14b
---

# Config field order: domain-first, ambient tail

## Context

The field-grouping sweep found two competing orders: Logger/Retry first
(producer, admin, batcher, the two alert configs) vs domain-first with a
Logger/Retry tail (every worker kind, every sub-consumer config, the
runner configs). ConsumerConfig had drifted worst -- Retry, ShutdownTimeout
and Logger buried mid-struct between the domain knobs and the Message
block.

## Decision

Standardize on the domain-first shape and codify it in CONVENTIONS.md:
domain fields grouped by concern with blank lines, then the ambient tail
Logger, Retry, then per-loop retry curves. It was already the majority
among configs with real domain content, and it matches the param-order
principle (ambient trails). The ~25 two-field {Logger, Retry} plumbing
configs are all tail and stay untouched. WithDefaults/Validate walk
declaration order; dependency-computed defaults (ShutdownTimeout from
MessageMax) trail their inputs.

Table columns got the same likeness pass on the baseline DDL:
cron_job.suspended moved beside schedule (both say when it fires);
delivery groups outcome state (status, attempts, can_run_after,
last_error) before the lease pair (lease_token, lease_until).

## Consequences

- Reordered: ConsumerConfig (+ShutdownTimeout/InstanceTTL alignment with
  its MessageConsumerConfig mirror), ProducerConfig, BatcherConfig,
  MessageAdminConfig, PartitionCountConfig, CompactionReadCostConfig.
- Two `RETURNING *` on lease (freshclaim, reclaim) now name their columns
  -- the SELECT * ban's silent-widening failure applied to RETURNING too.
- DDL verified by dev-DB drop+recreate plus cronlab and exceptionlab on
  the fresh baseline.
