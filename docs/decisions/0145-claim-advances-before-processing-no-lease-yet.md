---
status: superseded
date: 2026-06-26
phase: "6.5a"
---

# 0145 — `claimed` advances at claim time, before processing, with no lease

**Context.** `ClaimMessagesWithCursor` advances `claimed` atomically when the range is handed out, before any message is processed, and nothing yet records who holds the range or for how long.

**Decision.** Ship the happy path that way: claim advances the read frontier immediately; no lease, no reclaim.

**Consequences.** A crash after claiming but before committing strands `(committed, claimed]` — the next claim reads above `claimed` and skips those offsets, so they are lost to the group. This is the known, intended hole of a happy-path-only design, accepted to keep the claim a single `UPDATE … RETURNING`. Superseded by the range lease work, which exists precisely to make the claimed-but-uncommitted window crash-safe and reclaimable.
