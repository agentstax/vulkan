# TODO

Sliding window of in-flight work only. Future work lives in ROADMAP.md;
shipped work in HISTORY.md; decision rationale in DECISIONS.md ->
docs/decisions/.

## Log-event VK codes (14b, follow-on to [0558])

Shape agreed 2026-08-20: log events share the error codes' VK serial space
and registry; the code rides as a `code` attr (not message text); a log
event earns a code by the 0553 mirror -- operator-actionable enough for a
docs page (the Warn/Error set); docs URLs stay on the one /errors/ path.

- [ ] common.NewLogEvent(code, message, consequence) -> *LogEvent{Code,
      Message}; registered beside errors, `vulkan explain` lists both.
- [ ] ~10 declarations in owning vocabulary packages (logs.go): lease
      reclaimed, range quarantined, message dead-lettered, exception
      dead-lettered, kill backstop, options clamped, create-ahead
      failure, worker instance lost, manager row suspended, tick curve
      exhausted, cron ambiguous-commit republish.
- [ ] Call sites log the declaration's Message with "code", Event.Code.
- [ ] Hand-written docs page per code, same change (never generated).
- [ ] CONVENTIONS.md ## Logging: "when a log event earns a code"
      paragraph + `code` registry row.
- [ ] Start lines carry the resolved config facts the rule names —
      consumer adds work timeout, shutdown timeout, batch size (workers
      already carry rate); finishes [0558]'s snapshot rule.
- [ ] Decision record 0561.
