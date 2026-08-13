---
status: accepted
date: 2026-07-03
phase: "7"
---

# 0205 — Reads name their columns explicitly; SELECT * is out

**Context.** `readMessages` used `SELECT *` against `message_log`. When the
table grew a `routing_key` column that `MessageRow` had no field for,
`pgx.RowToStructByName` errored on the unmapped column — it does not silently
ignore extras — so the CURSOR read path was silently broken from the moment
the column landed until this phase's own live verification caught it.

**Decision.** `readMessages` selects explicit columns; `SELECT *` is
abandoned as a pattern.

**Consequences.** Additive schema changes stop being breaking for existing
readers: a new column is invisible to a query that never names it. This
incident settled the codebase-wide rule that every query names its columns.
**Rejected:** `SELECT *` — couples every reader's scan destination to the
table's full width, turning a column ADD into a runtime error.
