---
status: accepted
date: 2026-06-13
phase: "1"
---

# 0005 — Graceful shutdown lets the in-flight batch finish under context.WithoutCancel

**Context.** A shutdown signal cancels the worker's context. Propagating that
cancellation into a message currently being processed would abort its
transaction and roll back work that was about to complete.

**Decision.** On shutdown the in-flight batch finishes using
`context.WithoutCancel` plus a timeout; no new claims start.

**Consequences.** From the queue's point of view a graceful stop and a crash
are the same event — the transaction either committed or it didn't — so no
special shutdown state or drain protocol exists. The timeout bounds how long
shutdown can hang on a slow handler.
