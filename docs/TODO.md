# TODO

Sliding window of in-flight work only. Future work lives in ROADMAP.md;
shipped work in HISTORY.md; decision rationale in DECISIONS.md ->
docs/decisions/.

## [0634] ProduceInTx takes the message

Settled 2026-09-02, unbuilt. The four verbs complete the
{value, closure} x {producer-owned tx, caller-owned tx} table:
`ProduceInTx(ctx, tx, message, options)` becomes value-taking, today's
closure form is renamed `ProduceFuncInTx`, and `ProducerFunc` drops its
idempotencyKey param to `func(ctx context.Context, tx Tx) (*Message, error)`.
Pre-v1: both signature changes edit call sites in place.

### 1. Doc page first (the proposal)

- [x] Rewrite website/src/content/docs/guides/transactional-produce.mdx
      around the four-verb table: value `ProduceInTx` shows the multi-topic
      `InTransaction` block as a statement per topic; `ProduceFuncInTx`
      carries the payload-computed-from-reads case; closures shown as
      `(ctx, tx)`. Produce-last (lock on consumer progress) and the
      deadlock-ordering guidance stay on this page. Label ahead-of-library
      content Proposed. Write against website/VOICE.md.
- [ ] Review the page with the user BEFORE any Go change (the [0633] flow).
      Drafted 2026-09-02; lints clean (prettier, vale, remark, astro check).
      Beyond the four-verb rewrite the draft also: added the compacted-key
      deadlock-ordering paragraph (it only lived in ProduceInTx's Go
      comment), added the "Which verb" 2x2 table with a ProduceBatch line,
      moved the side-effects pointer + external-systems aside up to close
      "Several topics, one commit", and dropped the frontmatter claim that
      every sample compiles against the shipped library.

### 2. ProducerFunc drops idempotencyKey

- [x] pkg/producer/controller/datastore/insert.go: `ProduceFunc` type loses
      the idempotencyKey param; `runInsert` / `runInsertSavepoint` stop
      passing `data.IdempotencyKey.String()`; trim the type's key-dedup
      doc sentence.
- [x] Aliases needed no edit -- all three are pure type aliases whose doc
      comments already point at the datastore for the shape.
- [x] The two pkg/ passthrough closures dropped the param with it, so the
      root module builds: producer_instance.go:80 (chunk 4's first item,
      done early because the type change forces it) and
      schedule/producer/instance.go:138 (chunk 4 still deletes it whole).
      Root `go build ./...` green, `go test -race` 41 pass across
      pkg/producer, pkg/schedule, pkg/vulkan; tools/conventions 24 pass.
      examples/ is its own module and stays broken until chunk 5.

### 3. The verb rename + the value form

- [x] pkg/producer/producer_instance.go: value `ProduceInTx` builds the
      passthrough and delegates to `ProduceFuncInTx`, which keeps the old
      body. SETTLED: the key-lock / produce-last / deadlock doc stays whole
      on `ProduceInTx` (the verb most callers reach for) and
      `ProduceFuncInTx` points at it; the deadlock line now names
      `transactionFunc` so it can't be misread as the ProducerFunc.
- [x] pkg/vulkan/producer.go interface carries both verbs. The three
      `via ProduceInTx` comments (producer, controller, vulkan
      transaction.go) and schedule/producer/instance.go:77 read correctly
      for the value form -- no edit. Both SlowProduceThreshold comments
      reworded: ProduceFunc/ProduceFuncInTx include the caller's closure,
      both InTx verbs end at the insert, before the caller's commit.
- [x] Forced by the build, so done here: chunk 4's schedule item (the
      passthrough is deleted, `stored` passes straight to the value form).
      Root `go build ./...` green, `go test -race ./pkg/...` 120 pass in
      77 packages, tools/conventions green. No other nested module names
      these verbs (bench, reference, cmd, otelvulkan, tools all clean).

### 4. Internal call sites

- [x] Produce's keyed-path passthrough closure drops the third param
      (producer_instance.go) -- done in chunk 2, forced by the type change.
- [x] pkg/schedule/producer/instance.go: passthrough deleted, value
      `ProduceInTx` called directly -- done in chunk 3, forced by the build.
      The lock comment at :77 still reads correctly.
- [ ] pkg/consumergroup/messageconsumer/controller/datastore/fresh_claim.go:114
      SQL comment names ProduceInTx -- still true of the value form, but
      re-read the sentence once the verbs split.

### 5. Example call sites

- [ ] ~30 phase_1 labs: closure signatures drop `_ string` (mechanical).
- [ ] idempotencykeyslab/main.go:193 READS the minted key param -- the one
      real rewrite: supply its own key via ProduceOptions.IdempotencyKey
      per [0622] and assert on that.
- [ ] playground/02-produce-in-tx: multi-topic block becomes a statement
      per topic; header scorecard rewritten -- both "what hurt" lines
      (three-param closure, no value form / ROADMAP gap) are the gaps this
      change closes.
- [ ] playground/05-compacted-kv: head is read before the produce, so the
      value form fits; update header lines citing ProduceInTx's shape.

### 6. Doc-site sweep (shipped-behavior pages, same change as the build)

- [ ] quickstart.mdx:260-277 (closure sample + produce-last line),
      side-effects-and-retries.mdx:33,72 (closure ProduceInTx ->
      ProduceFuncInTx or value form), client.mdx:455 surface list,
      errors/VK0038.md:12 duration sentence. Drop the Proposed label from
      transactional-produce.mdx.

### 7. Verify + close out

- [ ] Foreground: `go build ./...`, `go test -race` on pkg/producer/...,
      pkg/schedule/..., pkg/vulkan; run playground 02 + 05,
      multitargetlab, idempotencykeyslab, one schedule lab on the dev DB.
- [ ] ROADMAP: remove the Later "ProduceInTx value-taking form" item and
      update the round-1 "still to build" line ([0635] remains).
- [ ] HISTORY entry citing [0634]; remove this TODO section.
