---
status: accepted
date: 2026-08-01
phase: "14"
---

# 0395 — The pending (head, xmax) pair is stored unconditionally on every poll

**Context.** The snapshot fence proves a (head, xmax) pair out when a later statement's snapshot `xmin` passes its xmax. Under nonstop producer traffic, a fresh pair never proves out inside its own poll — some transaction always overlaps — so proof must come from a pair persisted by an earlier poll.

**Decision.** The fresh pair writes to `pending_head`/`pending_xmax` unconditionally, not only when it fails to prove immediately. If storing were conditional, the next poll could find nothing to prove against and claims would stall behind traffic forever.

**Consequences.** Every claiming poll carries the small cursor-row write. Verified with an 8-producer sustained-load harness: progress while producing, 14k/14k messages drained, no stall.
