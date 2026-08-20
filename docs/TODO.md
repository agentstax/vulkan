# TODO

Sliding window of in-flight work only. Future work lives in ROADMAP.md;
shipped work in HISTORY.md; decision rationale in DECISIONS.md ->
docs/decisions/.

## Error anatomy implementation ([0550]; rules in CONVENTIONS.md ## Errors)

Design settled; this is the build. Per-chunk verification: build +
`go test -race` on touched packages + directly affected labs; full
fresh-DB suite at the review-ready checkpoint.

### Chunk 1 — common.Error

- [x] `pkg/common/error.go`: Error struct (code, recovery, problem, fix,
      values as named pairs, wrapped error), Recovery enum
      (Transient | Permanent), NewError constructor.
- [x] `With(...)` returns a copy carrying the values -- declared `Err*`
      variables stay immutable; a raise never mutates the declaration.
- [x] `Error()` renders `problem: name value, name value -- fix [code]`;
      empty fix drops the ` -- ` part; no values drops the `:` part;
      a wrapped cause renders after the code (`[VK0104]: cause`).
- [x] `Is` matches on code so `errors.Is(err, topic.ErrX)` keeps working;
      `Unwrap` returns the wrapped error (`Wrap(cause)` attaches it,
      copy semantics like With).
- [x] `LogValue()` (slog.LogValuer): code, problem, recovery, docs, fix,
      each value as its own attr, cause when wrapped.
- [x] `Docs()` derives the URL from the code; base URL is one const
      (`https://vulkan-5ss.pages.dev/errors/`).
- [x] NewError registers each code in a package-level registry at init;
      registry rejects duplicate codes and is the enumeration the tests
      below walk (`common.Errors()`).
- [x] Tests: tense-follows-recovery walk (Transient => problem starts
      "could not"; Permanent => never does), duplicate-code rejection,
      banned-word walk over problem lines ("failed", "invalid", "bad",
      "illegal", "unable", "unknown", "error", "please", "sorry", "!").

### Chunk 2 — recovery folded into retry

- [x] `classify` (pkg/common/retry_datastore.go) passes a `*common.Error`
      through unwrapped (recovery is already carried), then existing marker
      types, then IsTransientPgError; IsRetryable checks recovery FIRST.
      Tests: pass-through, marker preservation, unclassified wrapping,
      Wrap stops on Permanent / walks the schedule on Transient.
- Marker-type retirement moved to chunk 4 (its raise sites migrate there);
  no transitional layering survives past that chunk.

### Chunk 3 — declaration migration

- [x] Assign flat serial codes (VK0001+) to every named error variable in
      an errors.go. 17 total, domain order: common VK0001-0003
      (AlreadyConsuming, LifecycleContextNotCancellable, LeaseLost), topic
      VK0004-0007 (ConfigMismatch, NotFound, NotEmpty, NameTaken), admin
      VK0008-0009 (DestroyDisabled, ReservedTopicName), system VK0010-0011
      (SystemLive, TopicsRegistered), worker VK0012 (InstanceLost), cron
      VK0013 (CronJobNotFound), consumer/controller VK0014-0016
      (GroupNotFound, GroupLive, GroupDeliveriesPending), migrate VK0017
      (NotRegistered). All Permanent -- today's transients are pg errors
      classified by IsTransientPgError. Unexported internal signals
      (errPartitionsRemain, errRangeNotTracked) stay plain per the settled
      which-errors-get-codes rule. CONVENTIONS example updated
      VK0104 -> VK0005 (the real ErrTopicNotFound).
- [x] Rewrite each declaration via common.NewError with recovery + fix;
      doc comments keep the what-it-means/what-to-do wording.
- [x] consumer's lifecycleContextHelp block: kept as the long-help const
      appended below the one-line message (rule sheet: first line must
      stand alone).
- [ ] Walk-test coverage gap: the tense/banned-word tests live in
      pkg/common and only see codes registered in that test binary
      (VK0001-0003 + fixtures). Extend the walks to a home that imports
      every declaring domain -- decide at chunk 6, where docs generation
      needs the same full registry.

### Chunk 4 — raise-site sweep

- [ ] Raise sites of coded errors switch to `.With(...)` value pairs; the
      fix text moves from fmt.Errorf strings into the declaration.
- [ ] Wording sweep of remaining plain errors (validation fmt.Errorf) onto
      the CONVENTIONS templates -- one phrasal template per condition kind;
      "at least one item is required" -> `items must not be empty` etc.
- [ ] Wrapping audit: each layer adds only its own fact; no return+log
      double-reporting.
- [ ] Migrate the marker-type raise sites onto declared errors (swept
      2026-08-19): producer datastore partition.go createPartitionAhead
      (NewRetryableError on lock_timeout -> Transient declared error
      wrapping the cause), messageconsumer datastore commit.go
      (NewPermanentError on ambiguous Commit with outcomes -> Permanent
      declared error wrapping the cause), multitargetlab main.go
      assertion updated to the new shape.
- [ ] CHUNK CLOSER -- delete pkg/common/retry_error.go wholesale
      (RetryableError, PermanentError, IsRetryable). Classification is
      consulted, never encoded onto the error: Retry gains an unexported
      classification func field (default = recovery on the error),
      RetryDatastore sets recovery-then-IsTransientPgError, classify's
      wrapping goes away and errors surface bare. producer batcher
      resolve.go classifyBatchFailure switches off IsRetryable onto the
      same check. Grep proves zero marker references before the delete.

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
- [ ] docsBaseURL const (pkg/common/error.go): confirm the /errors/ path
      against the shipped site section and drop the placeholder TODO
      comment. If the project rename (ROADMAP Later) lands first, the VK
      prefix and base URL change together.
- [ ] Full fresh-DB lab suite; HISTORY.md entry citing [0550]; this TODO
      section and the ROADMAP item's settled sub-bullet removed.

### Open questions (decide when the chunk is picked up, record if they
### change [0550])

- SETTLED (chunk 1, 2026-08-19; none change [0550]):
  - Which errors get codes: only conditions declared as named `Err*`
    variables in an errors.go -- branchable or runtime-visible.
    Constructor/config validation stays plain fmt.Errorf on the
    CONVENTIONS templates: nothing branches on it, retry never sees it,
    a docs page per nil-check dilutes what a code means. A plain error
    that gains a runtime brancher gets promoted to a named variable.
  - Wrapped cause: attached via `Wrap(cause)` (copy semantics), renders
    after the code -- `problem: values -- fix [code]: cause` -- keeping
    the five parts contiguous and matching Go's `context: cause` chains.
    A With value that happens to be an error renders inline as text.
  - Docs base URL: `https://vulkan-5ss.pages.dev/errors/` (the deployed
    site); chunk 6 generates the pages under it.
