# TODO

Sliding window of in-flight work only. Future work lives in ROADMAP.md;
shipped work in HISTORY.md; decision rationale in DECISIONS.md ->
docs/decisions/.

## Log spam control on the error path (follow-on to [0558], designs [0564][0565])

Settled 2026-08-20: pkg/common/logging grows a private record pipeline
([0565]: record struct + one-method handler, stages in fixed order
capture -> suppress -> drain -> enrich -> sink, one assembly
constructor, Logger seam unchanged). Suppression ([0564]) is the
suppressHandler stage: instance-scoped state, key = (level, message),
Warn/Error only, 1-minute package-const window, repeats drop and count,
next same-key emit carries suppressed_count; no flush timer; renewal
heartbeat stays a repeating Warn.

- [ ] record + handler + the five stage types, each in its own file;
      bufferLogger/withLogger and LoggerWith's hoisting/idempotence
      guards dissolve into stages (ring + WithLogBuffer untouched).
- [ ] Settle the public entry points over the new internals at build
      (LoggerWith / BufferLogger signatures vs successors).
- [ ] Apply suppression once per long-lived instance (producer,
      consumer, systemmanager).
- [ ] suppressed_count row in CONVENTIONS.md's attr registry.
- [ ] Tests: window roll, count reset, level-in-key escalation,
      concurrent callers, suppressed Error keeps the ring for the next
      emitted one; log_buffer_test reshaped onto the pipeline; the
      heartbeat repeat collapses to first line + counted line per
      window (assert by level+attrs, never message text).
