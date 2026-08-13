---
status: accepted
date: 2026-08-02
phase: "14a"
---

# 0441 — Every domain gets the same three-layer package shape

**Context.** Domain packages had grown individual layouts, mixing read-models, validation, and SQL, so the same kind of code lived in a different place in every package.

**Decision.** Three layers per domain. `pkg/<x>` is vocabulary only: pure read-models, consts, error sentinels. `pkg/<x>/controller` is the only door to persistence: every public verb, the `to*` adapters that turn table shapes into vocabulary types. `pkg/<x>/controller/datastore` is table-exact SQL that trusts its inputs: `*Data` structs in model.go, every public method a retry-Wrap around a same-named private. Import arrows point strictly down.

**Consequences.** A schema change is visible in exactly one `*Data` file, and every domain reads the same way. Rolled across worker and topic first, then system, consumer, metrics, and cron; recorded as the Package-layout section of conventions.md, making violations bugs rather than style choices.
