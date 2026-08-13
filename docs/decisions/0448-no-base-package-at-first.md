---
status: superseded
date: 2026-08-04
phase: "14a"
---

# 0448 — No shared base package for the consumption-loop packages at first

**Context.** Splitting pkg/consumer into per-loop packages left roughly 150 lines of scaffolding that each loop needs. A shared base package was the obvious factoring.

**Decision.** No base package: the ~150 lines were copied into each loop package, per the standing rule that duplication beats abstraction — a shared package would have been an abstraction ahead of evidence.

**Consequences.** Each loop package stayed self-contained and free to diverge. Superseded by later work: pkg/consumer/base now holds the shared definition/execution scaffolding and the key-lease controller, once enough loops existed to show the shape the abstraction had to take.
