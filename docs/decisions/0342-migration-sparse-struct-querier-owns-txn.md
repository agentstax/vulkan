---
status: accepted
date: 2026-07-20
phase: "11.5"
---

# 0342 — A migration is a sparse struct of func fields, run against a Querier that cannot manage transactions

**Context.** Each migration step needs optional validate/up/down behavior, and the runner must guarantee a step's DDL and its version row commit atomically — a step that opens its own transaction would break that. Dated approximately; built across July 2026.

**Decision.** `Migration`/`TopicMigration` are sparse structs with func fields (`ValidateUp`, `Up`, `ValidateDown`, `Down`, plus a `NoTxn` bool). Steps receive a `Querier` interface that excludes `Begin`/`Commit`/`Rollback`, making it a compile error for a step to manage its own transaction; the runner owns the boundary and commits Validate + Up/Down + the version row atomically.

**Consequences.** A plain step needs no empty stubs, and transaction ownership is enforced by the type system rather than by convention. **Rejected:** an interface for migration steps — every plain step would carry empty stub methods.
