---
status: accepted
date: 2026-08-19
phase: 14b
---

# Dead-field pass: two deletions, one exemption

## Context

The config-refinement item's last sweep: every config/options field,
vocabulary read-model field, and datastore row-struct field audited for a
production reader; staticcheck (U1000) and unparam run over all three code
modules for dead vars/params. The codebase came back almost clean.

## Decision

Deleted, per the no-fields-without-readers rule:

- WorkerSnapshot.OldestInstanceAge and its whole chain (adapter line,
  WorkerSnapshotData.OldestInstanceAgeSecs, the oldest_instance_age_secs
  SQL expression). It was added for the worker state gauges, but the
  metrics collector shipped deriving MetricOldestUnclaimedAge from
  UnclaimedFor instead -- nothing ever read it. worker_instance.created_at
  stays: audit bookkeeping, like every other table's created_at.
- WorkerInstanceData.ExpiresAt: scanned from ClaimInstance's RETURNING,
  never adapted -- the vocabulary WorkerInstance never carried it (renew
  and release match on token, not on the expiry value).

Exempted: JobRequest.CronJobId has no in-library reader, but JobRequest is
the job_requests topic's WIRE PAYLOAD, not a read-model -- its reader is
user consumer code, and the id is the only stable way to correlate a
request to its cron_job row (a destroyed job's name is reusable). Payload
contract fields are judged by the wire contract, not the read-model rule.

## Consequences

- WorkerSnapshot is one field slimmer pre-v1; re-adding an
  instance-age gauge later starts from worker_instance.created_at, which
  survives.
- staticcheck/unparam found nothing else across root, cmd/vulkan, and
  otelvulkan -- the earlier 14b tier reviews left no other dead surface.
