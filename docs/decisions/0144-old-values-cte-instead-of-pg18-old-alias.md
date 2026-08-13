---
status: accepted
date: 2026-06-26
phase: "6.5a"
---

# 0144 — The claim reads the pre-update `claimed` via an `old_values` CTE, not PostgreSQL 18's `old` alias

**Context.** `ClaimMessagesWithCursor` must return the window `(low, high]`, which requires seeing the cursor's `claimed` value from before the `UPDATE` in the same statement. PostgreSQL 18 adds built-in `old`/`new` transition aliases in `RETURNING`, but depending on them pins the minimum server version.

**Decision.** Capture the pre-update value in a CTE named `old_values`, joined back via `FROM` so `RETURNING old_values.claimed AS low, cursor.claimed AS high` can see it. The name deliberately avoids `old` so PG18's built-in alias cannot shadow it.

**Consequences.** The query works on PostgreSQL versions below 18 and does not silently change meaning on 18+. **Rejected:** the PG18 `old` transition alias — an 18-only feature, and reusing the bare name `old` for the CTE would be shadowed by the built-in.
