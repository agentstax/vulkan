---
status: accepted
date: 2026-08-12
phase: "14a"
---

# 0471 — `RunCronJob` takes a fresh v7 idempotency key per call and defaults to concurrency 'allow'

**Context.** Operators need to run a job on demand, outside its schedule. A manual run is deliberately its own request: it must never be swallowed as a duplicate of the schedule's request, and it should run immediately by default rather than queue behind a running one.

**Decision.** `RunCronJob(name, cfg)` produces a request with a fresh random v7 idempotency key on every call, so no two run-now calls — and no run-now and scheduled request — ever dedupe against each other; only an ambiguous-commit replay of the same call can. Concurrency defaults to `'allow'`, with `'defer'` opt-in via `RunCronJobConfig.Concurrency`. Like any produce on the compacted topic, a run-now request supersedes a pending unclaimed request for the same job.

**Consequences.** Run-now always yields a real new request, and with the default `'allow'` it never takes or waits on a `key_lease`, so it runs beside an in-flight request rather than blocking on it. Operators who want overlap protection must opt into `'defer'` explicitly.
