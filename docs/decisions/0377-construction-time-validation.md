---
status: accepted
date: 2026-07-28
phase: "13"
---

# 0377 — Configs validate at construction via an exported WithDefaults-then-Validate pair

**Context.** `validate()` ran inside `Process()`, not `New()`, so a bad config surfaced at first use rather than at construction. The fix was generalized into a library-wide convention rather than a one-off answer for `MessageConsumer`.

**Decision.** Every config struct carries an exported `WithDefaults()` (fills unset zero fields, mutates and returns the receiver) plus `Validate()`, in its package's `config.go`. Every public constructor accepting a config resolves it in one fixed shape: nil-check required deps, accept nil/sparse cfg, `WithDefaults()`, `Validate()`, construct, returning `(T, error)`. Defaults resolve before validation — relational checks like `ShutdownTimeout > WorkTimeout + WorkTimeoutGrace + AckMargin` are meaningless against sparse input, and validating the resolved config catches defaults drifting into a bad relation. Checks crossing a dep and the config (`queue.Cap() >= BatchLimit`) stay in the constructor.

**Consequences.** Ripples all landed: `topic.Register` takes `*Config` (nil-allowed); `NewProducerDatastore`, both `NewConsumerDatastore`s, `NewPostgresDatastore`, `NewMessageProducer`, `retry.NewRetry`, and `NewDatastoreRetry` gained error returns; `retry.Policy` got its own `Validate` (a `MaxRetries < 1` policy makes `Wrap` return nil without calling the wrapped func — a silent fake success); nested policy errors wrap their field name. Exported (not unexported) was a deliberate reversal: `retry.Policy.WithDefaults` must be exported for cross-package callers, and one convention everywhere beats a visibility split. Known hole deferred: `options.go` setters still mutate config past validation with no error path.
