# 0565 -- logging grows a record pipeline under the Logger seam

status: accepted
date: 2026-08-20
phase: pre-v1

## Context

[0559]/[0561] built the debug buffer and enrichment as wrappers over the
four-method Logger interface. Adding suppression ([0564]) as a third
wrapper exposed the structure's limit: every concern reimplements four
methods, level is trapped in the method name instead of being data,
ordering lives in nesting enforced by hoisting and idempotence guards,
and composition is implicit in call order -- "buffered and suppressed"
has no declaration anywhere. One cross-concern bug was invisible in that
shape: a suppressed repeat Error would have drained and discarded the
held narration. Research: slog keeps its Logger thin and puts all
processing in the one-method Handler over a Record; zap's sampler
composes at the core (record) layer; Logback's DuplicateMessageFilter is
a typed filter stage in a chain; Kubernetes' EventCorrelator runs
filter -> aggregate -> dedup over the event in one component. No system
composes concerns on a leveled front-end.

## Decision

- Logger stays the user seam, unchanged. Below it, one shape:
  logging.NewPipelineLogger(sink, cfg) composes the stages a
  PipelineLoggerConfig declares -- Args (bound attrs), Buffer
  (WithLogBuffer boundaries), Suppress (repeat collapse). Zero config is
  a passthrough. Error-free like NewDefaultLogger; every config field is
  meaningful at zero.
- A sink that is already a *PipelineLogger merges instead of nesting:
  Args concatenate, Buffer/Suppress stay on once on, and the suppress
  stage carries over as the same chain node -- its window state lives on
  the handler and nowhere else.
- Internally every call becomes a record (loggedAt, level, message,
  args) walked through a slice of one-method stages in one fixed order:
  capture -> enrich -> suppress -> drain -> sink; a stage returning nil
  stops the walk. Capture before enrich keeps held records free of
  bound attrs (the ring holds value copies); suppress before drain
  keeps a dropped repeat Error from wasting the held narration.
- LoggerWith, BufferLogger, and the transformer-verb style die with the
  wrapper types (bufferLogger, withLogger) and the slog With fast path;
  ~70 call sites now declare their composition: configs {Buffer: true},
  instances {Buffer: true, Suppress: true}, identity binding {Args}.

## Consequences

- Narrows [0559]: its Logger-wrapper mechanism is replaced; its
  boundaries, ring, and drain semantics stand.
- Composition is declarative and greppable; a new concern is one stage
  file plus one config field.
