---
status: superseded
date: 2026-08-06
phase: "14a"
---

# 0401 — A message schema change is a new physical topic under the same name, never an in-place migration

**Context.** A user who changes their `Message` struct and redeploys hits Go's JSON decode failing silently — renamed or removed fields zero-value instead of erroring — and the failure surfaces late and non-deterministically: the retention tail decodes for days, mixed-version instances win claims arbitrarily during a rolling deploy, and parked exceptions replay weeks later into whatever struct exists then.

**Decision.** `topic.SchemaVersion` makes a breaking change a required, explicit, typed constructor parameter: a version bump is a brand new physical topic (own message log, own id space, own duties) registered under the same name. A consumer binds one version for its whole life.

**Consequences.** Isolation over cleverness — no per-row version column, no decode gating, no upgrader chains. The cost lands on compacted topics: latest-per-key survives retention by design, so a compacted topic cannot drain into a new version on its own; migrating one is explicit user-space work (the bridge consumer pattern) instead of something the library automates.

Superseded by [0618]: the schema version is a message_log column declared by the Message type; the topic catalog is keyed by name alone.
