---
status: accepted
date: 2026-08-19
phase: 14b
---

# The receiver letter is the initial of the type's final word

## Context

CONVENTIONS said the receiver "matches the type's initial", which is
ambiguous for multi-word types and was applied inconsistently (`i` on
`*Execution` types, `c` on `*BaseDefinition`, `f` on consumer
definitions, `s` on `*CronSchedulerDefinition`). The de-facto norm in
most of the codebase was already the final word's initial
(`*TopicDatastore` -> `d`, `*ControllerConfig` -> `c`).

## Decision

Codify: the receiver is the initial of the type's FINAL word. Swept the
violations: every `*Definition` -> `d`, every `*Instance` -> `i`,
`MetricsProducer` -> `p`, `result` -> `r`.

## Consequences

- CONVENTIONS.md states the rule with examples; new types follow it
  mechanically.
- Types whose receivers already matched the final word (claimBuffer's
  `b`, createAheadGate's `g`, executionPool's `p`) needed no change.
