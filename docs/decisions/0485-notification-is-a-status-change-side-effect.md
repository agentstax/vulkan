---
status: accepted
date: 2026-08-13
phase: "14a"
---

# 0485 — There is no notifier component: notification is a side effect of `Record` observing a status change

**Context.** Alert edges — became active, resolved — need to reach an operator. A notifier that remembers previous state in-process forgets it on restart and duplicates or drops edges.

**Decision.** `AlertController.Record` is the whole pipeline: read the head, `classify`, produce, and log WARN/INFO only when what was published differs in status from the head it replaced. Edges are derived from durable state, never remembered in-process. A nil finding still flows all the way through `Record` so an active head resolves rather than lingering.

**Consequences.** The pipeline is restart-proof and idempotent: a re-run reads the same head and publishes nothing new, and a repeat republish refreshes the head silently without logging a false edge. Invariant created: a run that finds nothing must still call `Record(ctx, name, owner, nil)` for every owner — short-circuiting the nil finding would strand active alerts unresolved.
