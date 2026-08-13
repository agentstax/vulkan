---
status: accepted
date: 2026-06-26
phase: "6.5a"
---

# 0141 — Two cursor frontiers (`claimed`, `committed`) carry the happy path with no per-message row

**Context.** The per-row `deliveries` design paid O(N) writes for N messages per group — an INSERT plus status UPDATEs each — even when every message succeeded. The overwhelmingly common case was buying per-row resolution it never used.

**Decision.** The `cursor` row grows from a single `position` into two `BIGINT NOT NULL DEFAULT 0` columns: `claimed` (the read frontier) and `committed` (the waterline). `ClaimMessagesWithCursor` advances `claimed` over a contiguous range in one `UPDATE … RETURNING` (`claimed = LEAST(claimed + $batch, MAX(id))`), the consumer reads exactly `(low, high]` from `message_log` ordered by `id`, and success is recorded by advancing `committed`. No per-message row is written. `lane`/`block_hi` columns are deliberately deferred.

**Consequences.** N successes collapse into advancing two integers on one row: O(N) row writes become O(1)-per-message in-place updates on a single row. The log splits into three zones — `<= committed` resolved, `(committed, claimed]` in-flight, `> claimed` unclaimed. A row is only ever paid for the exceptional fraction, which the later exception work handles. The in-flight gap is transient here and only becomes a persistent structure once leases and unresolved exceptions can pin it.
