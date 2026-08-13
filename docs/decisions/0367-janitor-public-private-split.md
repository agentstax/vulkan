---
status: accepted
date: 2026-07-28
phase: "13"
---

# 0367 — Janitor splits into a gated public entrypoint and a private loop

**Context.** Janitor maintenance is topic-scoped — one janitor serves every consumer group on the topic — but it was only reachable as part of a consumer's own loops, and the new lifecycle gate had to apply to public entrypoints without gating internal reuse.

**Decision.** Public `Janitor(ctx)` is gate plus merged ctx, documented as runnable in its own process or pod: a maintenance deployment beats N consumers redundantly ticking the same partition drops. Private `janitor(runCtx)` is the loop, and `Consume` runs the private one. The other exported loop methods (`Project`/`Process`/`RollWaterline`/`DrainExceptions`/`CursorClaim`) stay ungated side-doors for now — their fate belongs to the internal-package restructure. Raw `ConsumerDatastore` methods are deliberately ungated: plumbing layer, driven directly by labs and ops.

**Consequences.** The gate applies exactly once per public entry; internal callers reuse the loop without double-gating. Which process should own running the janitor (N groups still means N redundant loops) stayed open as a separate efficiency question.
