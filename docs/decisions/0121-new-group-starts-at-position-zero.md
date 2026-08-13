---
status: accepted
date: 2026-06-23
phase: "5"
---

# 0121 — A newly registered consumer group starts at position 0 and replays retained history

**Context.** Fan-out means many groups, each an independent `cursor` row over the shared `message_log`. A brand-new group needs an initial position: the earliest retained offset (replay everything) or the current head (only new messages).

**Decision.** `Register` calls `UpsertCursor(group)`, which lazily creates the group's cursor at `position = 0` — the column default. A new group therefore starts at the earliest retained offset and replays forward, without touching any other group's cursor.

**Consequences.** New groups get full history for free, which is the point of retaining the log; a group that only wants new messages must catch up through the backlog first. Group creation is idempotent and needs no separate provisioning step. Per-group lag is observable as `head − position` (`max(id)` from `message_log` minus the group's cursor).
