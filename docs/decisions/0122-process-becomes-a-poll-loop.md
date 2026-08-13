---
status: accepted
date: 2026-06-23
phase: "5"
---

# 0122 — `Process` is a continuous poll loop, not a single-shot pass

**Context.** The single-shot `ProcessV2` was fine for a one-pass replay demo, but useless for observing independent consumption — "slow group A keeps falling behind while B stays current" needs both groups polling continuously.

**Decision.** `ProcessV2` becomes `Process`, a poll loop: a `time.Ticker` on `PollRate` drives claim → process → advance each tick, with `ctx.Done()` to stop. `Claim` is the extracted per-batch body.

**Consequences.** Consumers become long-running processes with an explicit cadence knob (`PollRate`) and a clean shutdown path, and per-batch logic has one named home (`Claim`). Latency is bounded below by the poll interval rather than being event-driven.
