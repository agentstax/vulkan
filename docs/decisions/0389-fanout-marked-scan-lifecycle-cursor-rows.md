---
status: accepted
date: 2026-08-01
phase: "14"
---

# 0389 — FanOut tracks a per-group high-water mark on the cursor table; LIFECYCLE groups register cursor rows and pin retention

**Context.** `FanOut` rescanned the entire message log on every call — measured linear at 9.5ms/55ms/246ms median per tick at 10k/50k/200k rows — instead of tracking where it had already materialized deliveries.

**Decision.** The marked scan, with state living on the existing cursor table: LIFECYCLE groups now register a cursor row too, and FanOut advances `claimed = committed` to its hardened mark. Each tick is two autocommit statements: a read-only (head, xmax) pair plus cursor read with an idle short-circuit, then one statement that scans `id > committed` (LIMIT `FanOutBatchLimit`, default 1000), materializes matching deliveries, and hardens the mark. When the LIMIT cuts the scan short, the mark caps at the last id actually scanned, and every row counts against the LIMIT matched or not, so sparse-routing groups still progress. Unlike the claim gate, there is no `settled_head` term: mark and scan share one snapshot, so only fresh/stored pairs proven against the statement's own xmin may advance the mark; when nothing proves, `committed` holds and the tick rescans.

**Consequences.** The old rule "LIFECYCLE groups have no cursor row or they jam the retention floor" died: the floor now waits for messages a slow group hasn't fanned out yet, where before they could be TTL-dropped with their deliveries never materialized; a stopped LIFECYCLE group pins retention exactly like a stopped CURSOR group, with the same `AllowDropPastCommitted` override. Measured: steady-state flat ~130µs idle (one read-only statement) versus the old linear rescan; a tick materializing 100 fresh rows is ~1.2ms flat at every log size. Verified by stragglercheck and fanoutstress under `-race` (12k rows, 385 aborted-id gaps, 3 racing tickers × 3 groups, zero missing, zero mis-routed).
