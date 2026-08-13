---
status: accepted
date: 2026-06-23
phase: "5"
---

# 0126 — The poll loop waits one interval before its first claim

**Context.** `time.NewTicker(PollRate)` delivers its first tick only after one full interval, so a consumer entering the poll loop idles `PollRate` before its first read.

**Decision.** Accept the delay: enter the loop and wait for the first tick, matching the pattern of the parked V1 `Poll`. No immediate claim before the loop.

**Consequences.** First-claim latency equals `PollRate` — acceptable for lab use. If first-claim latency ever matters, the fix is a single immediate claim before entering the loop.
