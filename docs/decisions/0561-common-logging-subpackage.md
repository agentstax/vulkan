# 0561 -- Logging machinery moves to pkg/common/logging

status: accepted
date: 2026-08-20
phase: pre-v1

## Context

[0528] flat-merged the tier-zero packages into pkg/common when logging was
one 67-line file. [0558]/[0559] grew it to four files (~440 lines) of real
machinery -- interface, default handler, enrichment, buffer wrapper, ring --
and the flat package now mixes two mechanisms into its vocabulary. The
other groups keep their [0528] reasons to stay flat: a `common/errors`
package re-shadows stdlib errors (the alias dance), and retry cannot leave
-- MessageOptions embeds *RetryPolicy while RetryDatastore classifies via
common.Error, so a subpackage would cross-import its parent.

## Decision

- pkg/common/logging (package `logging`) owns the Logger seam: Logger,
  LoggerWith, NewDefaultLogger, BufferLogger, WithLogBuffer, the ring.
- Everything else stays flat in pkg/common; concurrency's destination
  remains internal/, never common/ -- the subpackage pattern is for
  public machinery only.
- toAttrs stays unexported on each side -- error.go keeps its copy, the
  ring's drain gets its own: duplication over a manufactured seam. common
  imports logging only where it consumes the Logger seam (lifecycle,
  RetryDatastore); the error anatomy depends on no machinery.
- Names travel unchanged (LoggerWith stays LoggerWith).
- CONVENTIONS.md: infrastructure kind reads "`common` and its machinery
  subpackages"; ## Logging references logging.Logger.

## Consequences

- ~315 qualifier renames across 130 files; config files import both
  common and common/logging -- a partial return of the multi-import
  [0528] removed, accepted for the physical grouping.
- Narrows [0528]: its logger merge is undone; the errors/retry/context
  merges stand.

Narrowed by [0563] (2026-08-20): the error anatomy, log events, and their
registry moved to pkg/common/diagnostic -- the stdlib-shadow objection
applied to the name "errors", not to the move.
