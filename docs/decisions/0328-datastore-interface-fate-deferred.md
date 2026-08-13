---
status: accepted
date: 2026-07-16
phase: "11"
---

# 0328 — The Datastore interfaces' fate was deferred to a standing cleanup phase, not decided mid-audit

**Context.** A boundary audit checked whether `pkg/consumer` leaks Postgres specifics past its `Datastore[Message]` interface. The method surface is almost entirely plain Go types — the one leak is `Commit`/`PartialCommit`'s `token pgtype.UUID` param and `LeaseRow.Token`/`ClaimedException.LeaseToken` — but the structural finding was bigger: `NewWorkConsumer` and `metrics.NewConsumerMetrics` take `*datastore.PostgresDatastore` directly and construct their concrete datastore internally, so no caller can actually hand in an alternate backend, unlike `pkg/producer`'s `NewWorkProducer`, which takes the interface. Testing is the only remaining justification for the interface, and the current shape does not clearly serve even that.

**Decision.** Keep, simplify, or remove was deliberately not resolved reactively while auditing a narrower question. It was recorded as the seed item of a standing, no-fixed-close code-cleanup phase, to be decided on its own.

**Consequences.** The `pgtype.UUID` leak and the unreachable interface persist until that deliberate decision. The alternative — patching the leak and declaring the audit done — would have papered over the real question of whether a backend-swappable interface is worth having at all.
