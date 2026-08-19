# TODO

Sliding window of in-flight work only. Future work lives in ROADMAP.md;
shipped work in HISTORY.md; decision rationale in DECISIONS.md ->
docs/decisions/.

## Error anatomy implementation ([0550]; rules in CONVENTIONS.md ## Errors)

Design settled; this is the build. Per-chunk verification: build +
`go test -race` on touched packages + directly affected labs; full
fresh-DB suite at the review-ready checkpoint.

### Chunk 1 — common.Error

- [ ] `pkg/common/error.go`: Error struct (code, recovery, problem, fix,
      values as named pairs, wrapped error), Recovery enum
      (Transient | Permanent), NewError constructor.
- [ ] `With(...)` returns a copy carrying the values -- declared `Err*`
      variables stay immutable; a raise never mutates the declaration.
- [ ] `Error()` renders `problem: name value, name value -- fix [code]`;
      empty fix drops the ` -- ` part; no values drops the `:` part.
- [ ] `Is` matches on code so `errors.Is(err, topic.ErrX)` keeps working;
      `Unwrap` returns the wrapped error.
- [ ] `LogValue()` (slog.LogValuer): code, problem, recovery, docs, each
      value as its own attr.
- [ ] `Docs()` derives the URL from the code; base URL is one const.
- [ ] NewError registers each code in a package-level registry at init;
      registry rejects duplicate codes and is the enumeration the tests
      below walk.
- [ ] Tests: tense-follows-recovery walk (Transient => problem starts
      "could not"; Permanent => never does), duplicate-code rejection,
      banned-word walk over problem lines ("failed", "invalid", "bad",
      "illegal", "unable", "unknown", "error", "please", "sorry", "!").

### Chunk 2 — recovery folded into retry

- [ ] `classify` (pkg/common/retry_datastore.go) checks `*common.Error`
      recovery FIRST, then existing marker types, then IsTransientPgError.
- [ ] Once every raised error carries recovery: retire RetryableError /
      PermanentError marker types and IsRetryable's AsType checks; recovery
      on the error is the one classification mechanism.
- [ ] Sweep marker-type call sites (grep NewRetryableError /
      NewPermanentError) before deleting the types.

### Chunk 3 — declaration migration

- [ ] Assign flat serial codes (VK0001+) to every existing named error
      variable across pkg/*/errors.go (common, topic, admin, worker, cron,
      consumer, migrate, system, producer, metrics, alert); record the
      assignment order in the chunk's commit.
- [ ] Rewrite each declaration via common.NewError with recovery + fix;
      doc comments keep the what-it-means/what-to-do wording.
- [ ] consumer's lifecycleContextHelp block: keep as the long-help const
      appended below the one-line message (rule sheet: first line must
      stand alone).

### Chunk 4 — raise-site sweep

- [ ] Raise sites of coded errors switch to `.With(...)` value pairs; the
      fix text moves from fmt.Errorf strings into the declaration.
- [ ] Wording sweep of remaining plain errors (validation fmt.Errorf) onto
      the CONVENTIONS templates -- one phrasal template per condition kind;
      "at least one item is required" -> `items must not be empty` etc.
- [ ] Wrapping audit: each layer adds only its own fact; no return+log
      double-reporting.

### Chunk 5 — CLI

- [ ] errorHandler branch renders any *common.Error as the block
      (error[code] / value lines / retry line when Transient / fix / docs).
- [ ] CLI fix vocabulary: per-code rewrite to a vulkan command that runs
      verbatim as pasted (translateAdminError grows into this seam).
- [ ] `--output json`: the five parts + recovery as one object.
- [ ] ADMIN_CLI.md transcripts re-checked against the new stderr shape.

### Chunk 6 — docs pages (review-ready checkpoint)

- [ ] website/: one page per code, headed by the verbatim problem text;
      generated from the registry, not hand-written.
- [ ] Full fresh-DB lab suite; HISTORY.md entry citing [0550]; this TODO
      section and the ROADMAP item's settled sub-bullet removed.

### Open questions (decide when the chunk is picked up, record if they
### change [0550])

- Which errors get codes: every branchable condition and CLI-visible
  failure, or also constructor/config validation errors? (Current lean:
  validation stays plain fmt.Errorf on the templates -- no caller
  branches on them -- but confirm before chunk 3.)
- Values on the one-liner when a value is itself an error (wrapped cause):
  where does the `%w` chain render relative to ` -- fix [code]`?
- Docs base URL const: real site path under vulkan-5ss.pages.dev vs
  placeholder until the site section exists.
