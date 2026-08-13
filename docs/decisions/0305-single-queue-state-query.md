---
status: accepted
date: 2026-07-14
phase: "10"
---

# 0305 — One queue-state query is the only derivation of the DB-truth health numbers

**Context.** Consumer health was previously visible only by querying Postgres directly (the ad hoc `just lag` recipe), and several new surfaces — snapshot, debug readout, OTel gauges — were about to need the same numbers at once.

**Decision.** One datastore method computes, live, per `(group, topic)`: backlog (`head - committed`), the `claimed - committed` inflight gap, `ready`/`inflight`/`dead` exception counts, oldest-unacked age, and open-lease count. Everything else reads through this one query; no other code re-derives any of these numbers.

**Consequences.** One mechanism per fact: the query is DB-truth, and every consumer of it (snapshot, readout, gauges) agrees by construction. Each number is an early warning for a distinct failure mode — backlog for lag, dead exceptions for true failures, oldest-unacked age for a single stuck message, open leases for a crashed consumer that never committed — so the query is the operational surface, not an internal detail.
