# 0565 -- logging grows a record pipeline under the Logger seam

status: accepted
date: 2026-08-20
phase: pre-v1

## Context

[0559]/[0561] built the debug buffer and enrichment as wrappers over the
four-method Logger interface. Adding suppression ([0564]) as a third
wrapper exposed the structure's limit: every concern reimplements four
methods, level is trapped in the method name instead of being data, and
ordering lives in nesting enforced by hoisting and idempotence guards
scattered across constructors. One cross-concern bug was invisible in
that shape: a suppressed repeat Error would have drained and discarded
the held narration. Research: slog keeps its Logger thin and puts all
processing in the one-method Handler over a Record; zap's sampler
composes at the core (record) layer; Logback's DuplicateMessageFilter is
a typed filter stage in a chain; Kubernetes' EventCorrelator runs
filter -> aggregate -> dedup over the event in one component. No system
composes concerns on a leveled front-end.

## Decision

- Logger stays the user seam, unchanged. Below it, pkg/common/logging
  rebuilds slog's Logger/Handler/Record split privately: an unexported
  record struct (loggedAt, level, message, args) and a one-method
  handler interface, handle(ctx, record).
- Each concern is one stage type in its own file: captureHandler (ring
  append below Error), suppressHandler ([0564]), drainHandler (first
  Error ships "preceding"), enrichHandler (bound args), sinkHandler
  (adapts the record back to the user's Logger).
- One constructor assembles the chain in the one fixed order:
  capture -> suppress -> drain -> enrich -> sink. Capture before
  suppress keeps the ring complete; suppress before drain keeps a
  suppressed Error from wasting the held narration; enrich after capture
  keeps held records free of the bound attrs the emitted line already
  carries.
- The library holds one Logger implementation: four one-line methods
  building a record and handing it to the chain head.
- bufferLogger, withLogger, and LoggerWith's hoisting and idempotence
  guards dissolve into stages. The ring, WithLogBuffer, and the boundary
  semantics of [0559] are unchanged.

## Consequences

- Public entry points (LoggerWith, BufferLogger, the suppression
  constructor) settle at build over the new internals -- signatures may
  survive; the wrapper types behind them do not.
- Narrows [0559]: its Logger-wrapper mechanism is replaced; its
  boundaries, ring, and drain semantics stand.
