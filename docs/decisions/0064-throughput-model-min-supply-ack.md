---
status: accepted
date: 2026-06-20
phase: "3"
---

# 0064 — Throughput is min(supply(batch), ack_capacity(workers))

**Context.** Measuring the pipeline's ceiling required a model of what bounds it: the single prefetch loop supplying messages, or the workers committing per-message results.

**Decision.** The mental model is `throughput = min(supply(batch), ack_capacity(workers))`. Claim cost is sublinear in batch size (~3.4µs/row asymptote, so a single prefetch loop can supply ~290k rows/s); at `batch=100` supply is round-trip-capped at ~17–18k/s, right at the ack ceiling. Raising batch exposes the ack wall (~20–22k msgs/s at 64 workers on this box), which scales with worker count.

**Consequences.** The prefetcher is not a hard ceiling until far beyond current throughput; the bottleneck is the per-message commit on the success/failure path. How it was diagnosed: `pg_stat_activity` wait-event sampling, plateau when raising batch (not supply-bound), and scaling with workers at fixed large batch (commit-path-bound). **Rejected:** "the single prefetch loop is the ceiling" — an earlier wrong conclusion drawn from measuring only `batch=100`.
