# 0558 -- Logging rule sheet

status: accepted
date: 2026-08-20
phase: pre-v1

## Context

108 log call sites had grown with no written rules: Warn was the de facto
error level (54 Warn / 3 Error), the same concept went by different attr
keys (`error`/`err`, `topic` holding ids and names, `group`/`group_id`/
`consumer_group`, `owner`/`system`/`scope`), two paths logged non-constant
messages, and the default logger wrote to stdout. Research (Stripe
canonical lines, River/Temporal/NATS/K8s, OTel/slog standards, journald,
OBS/Factorio) converged on: Info = state transitions only, level = who
must act, static messages + fixed key vocabulary, log-or-return-never-both.

## Decision

CONVENTIONS.md `## Logging`: logs and errors are one message system.
- Levels by "who must act": Error = machinery stopped, no caller receives
  an error (tick streak past the backoff curve's cap escalates Warn->Error);
  Warn = degraded-but-self-healing or data consequence; Info = lifecycle
  transitions and completed admin verbs; Debug = per-item narration.
  Silent steady state; counts, never per-row lines.
- Messages: static lowercase clauses under the error problem-line rules
  (banned words, could-not tense, ` -- ` consequence). Nothing branches on
  message text; labs count by level + attrs.
- One attr registry in the section (error as a value -- never `.Error()`
  first, LogValue renders the five parts; topic/topic_id name-vs-id split;
  `<verb>_count`).
- Identity bound once via LoggerWith at instance construction; start lines
  are diagnosis snapshots (vulkan_version + resolved config facts), stop
  lines carry nothing extra.
- Default logger: stderr (was stdout), WARN and up -- keeps 0304's
  working-default stance, frees stdout for program output.
- `vulkan explain <code>` renders any declared condition offline from the
  registry (journald catalog / rustc --explain shape).

## Consequences

Swept all 108 sites (reclassifications, keys, grammar); deleted the
log-and-return doubles (retry-exhausted Warn, batcher grace Warn, hard-
timeout Warn); alert paths log static captions with the alert message as
an attr. The ~40 Logger field comments resolved to one wording in the
same sweep. Rejected: OTel dotted attr namespaces (flat snake_case kept,
otelvulkan can map at its bridge); DiscardHandler default (quickstarts
would lose failure visibility).
