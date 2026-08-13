---
status: accepted
date: 2026-07-20
phase: "11.5"
---

# 0344 — System scope and topic scope version independently

**Context.** The schema has two kinds of surface: shared control-plane tables, and per-topic table families that each topic carries its own copy of. One version counter cannot describe both — a system-only change says nothing about any topic's tables. Dated approximately; built across July 2026.

**Decision.** Two independent version counters, both baselined at v1: system scope for the shared control-plane tables, topic scope for the per-topic table families. A topic's version advances only when a topic-scope step actually runs for that specific topic.

**Consequences.** A system-only migration never touches any topic's version, and topics can sit at different versions mid-rollout — the version record describes what each entity actually has, not what a global counter claims.
