---
status: accepted
date: 2026-06-23
phase: "4"
---

# 0102 — The cursor claim must `ORDER BY id`; an unordered claim silently loses messages

**Context.** The first cut of `ClaimMessagesV2` read `WHERE id > $1 LIMIT $2` with no `ORDER BY`. SQL guarantees no row order without one, so `LIMIT` returns an arbitrary N rows. Since processing advances the cursor to each returned `id`, the high-water mark can jump past unread offsets: cursor=0, ids 1–5, limit 2 returning {4,5} drives the cursor to 5 and drops 1–3 forever. It passed casual testing only because a small table happened to get a forward primary-key index scan.

**Decision.** `ClaimMessagesV2` reads `SELECT … WHERE id > position ORDER BY id LIMIT N`. The `ORDER BY id` is load-bearing, not cosmetic: a high-water mark is only correct over an ordered claim.

**Consequences.** Establishes the standing invariant that every cursor-advancing read is ordered. Any future claim variant that drops the ordering silently breaks the cursor abstraction with no error at claim time — the failure mode is permanent, invisible message loss.
