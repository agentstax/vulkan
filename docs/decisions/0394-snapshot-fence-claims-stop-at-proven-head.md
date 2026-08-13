---
status: accepted
date: 2026-08-01
phase: "14"
---

# 0394 — Cursor claims stop at a proven head via a snapshot fence, not the visible MAX(id)

**Context.** `BIGSERIAL` assigns ids at INSERT time but transactions commit in any order, so the log's visible `MAX(id)` can sit above a still-uncommitted lower id. FreshClaim advanced `claimed` to that raw head and nothing ever re-reads below `claimed`. Proven live with two connections: A holds id=8 uncommitted, B commits id=9, a claim takes (0,9] seeing only 8 rows, A then commits — the next claim returns nil "caught up" and message 8 is lost forever. (The LIFECYCLE path was proven immune in the same harness: its rescan self-heals stragglers.)

**Decision.** Each poll reads `MAX(id)` and the snapshot's `xmax` in one statement (one snapshot, so the pair is sound: any transaction that could own an id at or below head was already running, hence its txid < xmax), taken before the claim transaction's first write so our own txid isn't in the waited-on set. The claim's `gate` CTE takes the best of the cached `settled_head`, the fresh (head, xmax) pair, and the stored pair — a pair proves out when the current statement's snapshot `xmin` passes its xmax: every transaction that could own an id at or below it has finished, so each row is visible or its id is a permanent gap. Three new cursor columns carry the state: `settled_head`, `pending_head`, `pending_xmax xid8` (migration edited in place).

**Consequences.** Claims lag the raw head by the lifetime of the oldest overlapping produce transaction — a long-held `ProduceInTx` holds claims back for its duration — accepted as the honest floor, since those ids aren't provably safe to seal over. Measured (cursorbench): idle at parity; claiming pays one extra round trip (+74µs at 10 partitions, +12% at 500, amortized across up to `limit` messages), still well under the old two-MAX shape at 500 partitions. **Rejected:** an advisory-lock fence producers must cooperate with — topic-wide produce stalls queued behind one slow `ProduceInTx`. **Rejected:** an internal outbox/inbox table — write amplification plus a group registry. **Rejected:** row-level `xmin` filters — only visible rows can be examined, and txid order != id order under `ProduceInTx` (live counterexample constructed). **Rejected:** lag windows — timing assumptions.
