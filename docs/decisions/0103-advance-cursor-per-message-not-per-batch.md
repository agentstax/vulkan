---
status: accepted
date: 2026-06-23
phase: "4"
---

# 0103 — The cursor advances after each message, not once per batch

**Context.** After claiming a batch, the consumer could advance the cursor once at the end (`UPDATE position = $last`, one round-trip) or after each processed message (N round-trips).

**Decision.** Advance after each message: process a message, then `MoveCursor` to its `id`, message by message through the batch.

**Consequences.** Costs N cursor updates per batch but gives a tighter at-least-once checkpoint — a crash mid-batch reprocesses only from the last committed message, not the whole batch. Correct only because the batch is ordered (`ORDER BY id`). **Rejected:** once-per-batch advance to the last id — cheaper, but a mid-batch crash would replay the entire batch; granularity was chosen over round-trips on purpose. The same choice is carried forward when the waterline (`committed`) later replaces `position`.
