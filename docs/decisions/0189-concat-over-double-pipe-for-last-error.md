---
status: accepted
date: 2026-07-03
phase: "6.5c"
---

# 0189 — String building in SQL uses concat(), not ||

**Context.** The exception path builds `last_error` strings in SQL; Postgres
offers both the `||` operator and the `concat()` function.

**Decision.** Use `concat()`.

**Consequences.** `||` is visually confusable with logical OR for readers
coming from C-family languages; `concat()` reads unambiguously at the cost of
a slightly longer call.
