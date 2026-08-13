---
status: accepted
date: 2026-08-06
phase: "14a"
---

# 0404 — Message row metadata reaches consumer functions via consumer.MetaFromContext, not a signature change

**Context.** A bridge consumer re-producing messages needs the source row's identity — its id (to derive an idempotency key), its compaction key and rank, its routing key — which the deserialized message body does not carry.

**Decision.** `MessageMeta` carries `Id`, `RoutingKey`, `CompactionKey`, `CompactionRank`, `CreatedAt`, and is read with `consumer.MetaFromContext(ctx)`. An unexported context key is set before `callSafely` on both the cursor-claim and exception-claim paths. It deliberately does not carry the idempotency key.

**Consequences.** Consumer function signatures stay unchanged; metadata is opt-in per call site. The unexported key means the only read path is the accessor, keeping the surface one function.
