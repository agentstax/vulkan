---
status: accepted
date: 2026-08-19
phase: pre-v1
---

# 0552 -- A missing system topic raises migrate.ErrNotRegistered

## Context

A missing `__system.*` topic raised two different errors: metrics and alert
paths raised topic.ErrTopicNotFound with a "run RegisterSystem first" hint
interpolated into the message; cron paths raised migrate.ErrNotRegistered.
Two mechanisms for one fact -- and under [0550]'s anatomy the hint can no
longer ride the raise site: ErrTopicNotFound's declared fix (register it
with MessageAdmin.RegisterTopic) is actively wrong for reserved topics,
which RegisterTopic refuses (ErrReservedTopicName). The CLI already
translated both errors to the same "system not registered" message.

## Decision

Every missing `__system.*` topic raises
migrate.ErrNotRegistered.With("topic", ...) -- the condition truly is "the
system was never registered", and that error's declared fix (register the
system with MessageAdmin.RegisterSystem first) is exactly right. Raise
sites: admin metrics/alert/cron status paths, otelvulkan's resolveTopicId,
the alert provisioners. A missing user topic keeps ErrTopicNotFound.

## Consequences

Doc comments now promise migrate.ErrNotRegistered until RegisterSystem has
run; the CLI's manager_run branch on ErrTopicNotFound switched to
ErrNotRegistered (rendered output unchanged). Pre-v1 API contract change:
callers branching on ErrTopicNotFound for system topics must branch on
ErrNotRegistered.
