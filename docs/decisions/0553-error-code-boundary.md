---
status: accepted
date: 2026-08-19
phase: pre-v1
---

# 0553 -- Which errors get codes: the declaration boundary

## Context

[0550] defined the anatomy but not which conditions get declared: the rule
lived only in a deleted TODO note, and "why is this errors.New not coded?"
had no citable answer. Coding everything fails too -- validation guards
have no branchers, never reach retry machinery, and their fix restates the
problem, so hundreds of near-identical codes would dilute the registry and
the docs (SQLSTATE codes server conditions, not client argument checks).

## Decision

A condition earns a declared named error variable (and code) by any one of:
a caller in another package branches on it with errors.Is; its recovery
must differ from what IsTransientDatastoreError concludes on its own; it is
a user-facing condition worth a docs page. Constructor/config validation,
internal invariant guards, and unexported same-package control-flow signals
stay plain errors on the wording templates; a plain error is promoted the
moment it crosses the boundary. Codified in CONVENTIONS.md ## Errors.

Audit of all ~618 raise sites against the boundary promoted five:
errPartitionsRemain -> topic.ErrTopicPartitionsRemain VK0020 (its surfaced
message carried user advice in prose); the topic and worker
deleted-mid-declaration races -> ErrTopicDeclarationInterrupted VK0021 and
worker.ErrDeclarationInterrupted VK0024, both Transient because their Wrap
closures re-read on retry, so DatastoreRetry now heals races that
previously surfaced as permanent one-shots; the schema version-skew guards
-> migrate.ErrSchemaOlderThanBuild VK0022 / ErrSchemaNewerThanBuild VK0023
(operator conditions with real fixes, raised from three sites); and two
"topic %d is not registered" prose duplicates now raise the existing
topic.ErrTopicNotFound. Timeout/panic texts recorded into delivery_log,
"no cursor" invariants, and the otelvulkan reserved-prefix validation
stay plain -- no brancher, default recovery correct.

## Consequences

Five hand-written docs pages (VK0020-VK0024). The audit is repeatable:
branch-target enumeration plus an advice-verbs grep over raise strings.
