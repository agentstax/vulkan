---
status: accepted
date: 2026-07-12
phase: "9"
---

# 0290 — Timeout errors record the message id in last_error, never the work payload

**Context.** A timeout error needs enough identity to correlate with the message that hung. `last_error` is DB-persisted with far wider read access than an in-process error ever had, and payloads can carry sensitive data.

**Decision.** Timeout errors tag `last_error` with `messageID` only — the raw `work` payload is never written into it.

**Consequences.** Operators can join `last_error` back to the message by id when they have log-read access; payload contents never leak into a column whose readership was not designed for them.
