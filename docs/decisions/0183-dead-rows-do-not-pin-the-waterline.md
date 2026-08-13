---
status: accepted
date: 2026-07-03
phase: "6.5c"
---

# 0183 — The waterline pins below unresolved exceptions; dead rows do not block it

**Context.** With failures recorded as individual `deliveries` rows,
`committed` must not certify an offset whose retry is still pending — but it
also must not stall forever on a message that will never succeed.

**Decision.** `AdvanceWaterline` gains a second blocker term: the target is
`LEAST(min open-lease low, min unresolved ready/inflight exception's
message_id − 1, claimed)`. Rows in `dead` are excluded — they do not block.

**Consequences.** `committed` certifies terminal resolution, not success: a
dead-lettered message is finished as far as the frontier is concerned, so
poison cannot pin the waterline forever. Because `committed` is a single
prefix mark, offsets that succeeded above the lowest blocker stay
uncertified until that blocker resolves. The term is generic over any
unresolved exception, whatever put the row there — both terms and `claimed`
must be read in the advance's single snapshot SELECT (see 0162).
