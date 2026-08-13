---
status: accepted
date: 2026-08-06
phase: "14a"
---

# 0407 — The bridge derives its IdempotencyKey deterministically from the source message id

**Context.** A bridge consumer that crashes and restarts resumes from its persisted cursor and re-produces messages it may already have written into the target topic. A fresh-per-attempt key would make every re-produce a new row.

**Decision.** The bridge's `IdempotencyKey` is derived deterministically from the source message's id (UUIDv5-style), so producing the same source message twice yields the same key and the second write is a no-op.

**Consequences.** Crash-restart safety with no bridge-side bookkeeping beyond the ordinary consumer cursor: schemaevolutionlab proves a crashed-and-restarted bridge resumes with no duplicate rows in the target topic. The derivation is the pattern's load-bearing detail — a bridge written with fresh keys silently duplicates on every restart.
