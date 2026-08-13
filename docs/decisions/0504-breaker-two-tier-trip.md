---
status: accepted
date: 2026-07-28
phase: "13"
---

# 0504 — The breaker trips per instance; global OPEN is a quorum of locally-open instances

**Context.** One instance's evidence cannot distinguish "the dependency is dead" from "I am broken" — a corrupted host file on one bad node produces a 100% systemic-looking failure rate from inside that instance, and letting it trip a group of hundreds would be a false global outage. So evidence stays local, and so does the first verdict.

**Decision.** Tier 1: an instance whose own batches run all-systemic for N consecutive ticks opens its own breaker — stops claiming, stops exception-claiming, self-probes on cooldown. Locally open means zero new attempts by that instance: no new claims, no exception retries, no attempts against already-claimed or buffered work once the open state is known — only a message already inside `consumerFunc` runs to completion (goroutines cannot be killed). Tier 2: a genuinely dead dependency trips every instance within a few ticks, so the global signal is K distinct instances locally open — a quorum of local verdicts, written to a shared breaker row keyed (topic_id, group). Global OPEN unlocks the collective machinery (single prober, reconciliation, group-level metric), not the pause itself — even without tier 2, a real outage converges to everyone-paused.

**Consequences.** Correct for both causes: a bad-node instance stops converting healthy messages into failures, and its parked rows get retried by healthy instances through the exception window — the system self-heals around a bad node with zero coordination, and "instance X open while the group is closed" is exactly the bad-node alert an operator wants. Breaker state is never a hot-path query: an async ticker refreshes a shared atomic the claim/exception loops read; staleness costs one ticker period of extra claiming, mopped up by reconciliation. Hand-back gap: the unattempted remainder returns only via lease expiry, which increments reclaims — flapping would accumulate toward the MaxRangeReclaims poison quarantine, so reconciliation refunds reclaims alongside attempts. Open question recorded: K as a small absolute (brittle: unanimity for tiny groups) versus a fraction of live instances — the fraction needs the live-instance count from the presence design's heartbeat rows, making this the first feature with a hard dependency on it.
