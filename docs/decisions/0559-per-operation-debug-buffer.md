# 0559 -- Per-operation debug buffer

status: accepted
date: 2026-08-20
phase: pre-v1

## Context

Debug narration is invisible in production (default WARN), so a failure
line arrives without the story that led to it. Every mature system keeps
a cheap always-on ring surfaced only on failure (JFR, Go 1.25
trace.FlightRecorder, Sentry breadcrumbs, .NET log buffering, Monolog
fingers-crossed); the Go slog ecosystem has no established package for
it, and only the library knows its own operation boundaries.

## Decision

pkg/common ships the buffer at the Logger seam, not slog.Handler:
- `WithLogBuffer(ctx)` opens an operation boundary carrying a bounded
  ring (64 records, oldest dropped, dropped_count kept).
- `BufferLogger(inner)` wraps any `common.Logger`; every config's
  WithDefaults applies it to the resolved Logger. Wrapping is idempotent
  and `LoggerWith` hoists the wrapper outermost, so enrichment chains
  never double-append.
- Debug/Info/Warn records inside a boundary are held AND forwarded; the
  operation's first Error record drains the ring into its `preceding`
  group attr (numbered subgroups: logged_at, level, message, own attrs).
  The failure line ships its own narration through any handler, because
  it rides an Error-level record.
- Boundaries: ProducerInstance.Produce/ProduceBatch, BaseConsumer.
  CallSafely (per-delivery dispatch), InstanceTickRunner per tick. A ring
  that never drains dies with its ctx.

## Consequences

Operators get the failed operation's Debug story with zero standing
volume; concurrent operations cannot contaminate each other (per-ctx
scope -- the Monolog worker-mode lesson). Cost when healthy: one ctx
lookup + append of already-built args per buffered call. Rejected:
slog.Handler-level replay (only works for *slog.Logger callers, needs
Record.Clone contract); Warn as a drain trigger (routine self-heal warns
would carry buffers) -- revisit per-event if a Warn proves to need it.

Narrowed by [0565] (2026-08-20): the BufferLogger wrapper is replaced by
pipeline stages; the boundaries, ring, and drain semantics stand.
