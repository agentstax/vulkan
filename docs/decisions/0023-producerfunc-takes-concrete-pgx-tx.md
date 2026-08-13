---
status: accepted
date: 2026-06-14
phase: "1.5"
---

# 0023 — ProducerFunc takes a concrete pgx.Tx

**Context.** The producer callback needs a transaction handle. Passing a
concrete `pgx.Tx` couples an otherwise datastore-agnostic package to pgx;
avoiding that requires a driver-neutral interface with no second backend to
justify it.

**Decision.** `ProducerFunc` takes `pgx.Tx` directly. Accepted deliberately —
pgx is the only backend. If a second backend ever appears, extract a
driver-neutral Querier interface plus an adapter; a TODO marks the spot at
`pkg/producer/producer.go`.

**Consequences.** Callers import pgx to write a producer callback. No
speculative abstraction to maintain; the escape hatch is named and located.
**Rejected:** a driver-neutral interface now — indirection with exactly one
implementation.
