---
status: accepted
date: 2026-08-12
phase: "14a"
---

# 0463 — Cron concurrency is enforced at consume time by the key lease, never by the scheduler

**Context.** A `cron_job` declares concurrency `'allow'` or `'defer'`. Overlap between a running request and a newly due one has to be prevented somewhere, and the key-lease machinery already prevents overlap for any keyed message at consume time.

**Decision.** The scheduler does not enforce concurrency at all. It stamps the job's concurrency into `MessageOptions` on the produced request, and the shipped `key_lease` machinery enforces it at consume time, exactly as for any other keyed message.

**Consequences.** One enforcement point; the scheduler stays a dumb producer and never inspects delivery state. **Rejected:** scheduler-side last-message-resolved checks — a dual enforcement layer for overlap, which `key_lease` already owns.
