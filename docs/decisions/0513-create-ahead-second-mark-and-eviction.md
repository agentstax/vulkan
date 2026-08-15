---
status: accepted
date: 2026-08-14
phase: "14"
---

# 0513 — Create-ahead ships with the 95% backstop mark, and destroyed topics evict their claim entry

**Context.** [0512] deferred the second mark to "only if the drop warn ever
shows up in practice" and left `createAheadAttempted` entries live forever
(bounded by distinct topics, stale after a destroy). Building it, both were
pulled forward: the second mark is cheap (one more integer compare per
append), and the eviction is one `Delete` at a spot the producer already
learns the topic is gone.

**Decision.**
- Both marks ship now: 80% and 95% of each partition. The claim sequence
  becomes `partition*2` for the 80% mark and `partition*2 + 1` for the 95%,
  keeping one monotonic per-topic CAS value while letting the 95% attempt
  win a fresh claim after the 80% one already advanced it. The 95% mark is
  the in-process backstop for an 80% attempt that failed and dropped.
- A create-ahead goroutine whose failure is undefined_table (the topic's
  parent table is gone -- destroyed while the goroutine was in flight)
  deletes the topic's `createAheadAttempted` entry instead of warning, so
  the map stays bounded by live topics.

**Consequences.** Amends [0512]'s second-mark deferral; everything else
there stands. A partition now gets up to two proactive attempts before the
boundary heal; the drop warn now signals both marks failing, a stronger
signal than before. Eviction only fires if a mark triggers after the
destroy -- an idle topic's entry lingers harmlessly until then.
