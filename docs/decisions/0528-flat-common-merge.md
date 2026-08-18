---
status: accepted
date: 2026-08-17
phase: 14b
---

# pkg/logger, pkg/retry, pkg/errors, pkg/context merge into a flat pkg/common

## Context

Five tier-zero packages sat beside pkg/common: logger (126 importers), retry
(84), errors (7), context (1), concurrency (5). Nearly every file imported
the common+logger+retry trio; pkg/errors and pkg/context shadowed stdlib
names, forcing vulkanerrors/vulkanctx aliases. The domain-vs-plumbing seam
cuts through retry itself (RetryPolicy is domain data serialized inside
MessageOptions; the executors are plumbing), so a two-package split would
keep a cross-import — and common already imported retry.

## Decision

- Flat merge into pkg/common; subpackages rejected (common/logger is still
  package logger to Go — path-only grouping changes nothing callers see).
- Renames: retry.Policy -> common.RetryPolicy; retry.DatastoreRetry ->
  common.RetryDatastore (user-set, matches the <X>Datastore scheme) with
  NewRetryDatastore; logger.With -> common.LoggerWith (bare With says
  nothing). Everything else keeps its name (Logger, NewDefaultLogger,
  Retry, sentinels, LifecycleContext).
- Files named for what they declare: logger.go, lifecycle.go, errors.go,
  retry.go, retry_policy.go, retry_datastore.go, retry_error.go;
  messageoptions.go renamed message_options.go and ConcurrencyPolicy split
  out to concurrency_policy.go.
- concurrency stays excluded: the public-surface trim hides it entirely, so
  its destination is internal/, not the public vocabulary.

## Consequences

- One tier-zero import; the vulkanerrors/vulkanctx alias dance is gone
  (labs importing examples/phase_1/common alias the pkg as vulkancommon).
- The trim's retry demotions become unexports inside common.
- CONVENTIONS.md now reads common.Logger / pkg/common where it said
  logger.Logger / pkg/errors.
