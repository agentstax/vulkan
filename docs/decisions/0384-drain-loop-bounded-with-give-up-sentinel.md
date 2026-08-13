---
status: accepted
date: 2026-08-01
phase: "14"
---

# 0384 — The partition drain loop is bounded, giving up via an unexported sentinel

**Context.** A live producer's missing-partition self-heal resurrects partitions mid-drain, so against a topic still receiving writes the drain loop would never see the catalog reach zero. The old single-transaction `Destroy` did not have this hazard — it won the lock race atomically — and batching traded that away.

**Decision.** The drain loop is bounded at (starting partition count / batch size) + 3 passes. Past the bound it returns the unexported sentinel `errPartitionsRemain`; `deleteTopic` wraps only that case with the topic-flavored message "a producer is likely still writing; stop producers and call Destroy again," while other drain errors pass through unwrapped — the generic/domain split that keeps the drain reusable across parent tables.

**Consequences.** A caller-coordination bug (destroying a topic producers still write to) surfaces as an error instead of a livelock. The sentinel being unexported means `Destroy` callers cannot `errors.Is` for it — left open deliberately, to be decided alongside the presence-gate design, which would replace this error path with an up-front refusal naming the live producer; the bound remains the hard backstop either way.
