# TODO

Sliding window of in-flight work only. Future work lives in ROADMAP.md;
shipped work in HISTORY.md; decision rationale in DECISIONS.md ->
docs/decisions/.

## The typed topic handle

Settled 2026-09-04 in discussion. `client.Topic[Message](name)` roots
the typed tree; a message key is a handle under it; the CLI reads
through a raw payload type. Absorbs the ROADMAP Now item "Register
returns what you run, the client holds the assemblers" -- the consumer
and producer Register verbs move in the same change.

The shape being built:

```go
orders := client.Topic[OrderPlacedV1]("orders")   // type argument required

orders.Register(ctx, cfg)                          // admin verbs ignore the type
orders.Destroy(ctx, options)
orders.CompactionHeads(ctx, limit)                 // []*Message[OrderPlacedV1]
orders.Key("order-42").CompactionHead(ctx)         // ErrCompactionHeadNotFound when absent
orders.Key("order-42").Messages(ctx, limit)

orders.Producer().Register(ctx, cfg)               // *ProducerInstance[OrderPlacedV1]
orders.Group("billing").Register(ctx, cfg, handler)

client.Scheduler("nightly").Register(ctx, orders.Name, cron, &ReportRequestedV1{}, nil)
                                                   // unchanged: Message inferred from the payload
client.Topic[vulkan.RawPayload]("orders")          // the CLI and admin-only callers
```

Settled points, so the steps below do not reopen them:

- `Key(messageKey)` is the resource, not a `Compaction()` sub-handle:
  the key is a message property with two readers [0612], and lease
  facts land on the same handle later.
- `CompactionHead` returns the declared not-found error; only `Get`
  verbs are comma-ok. `AlertHandle.Get` maps it back to `(nil, nil)`.
- `common.RawPayload []byte` declares `SchemaVersion() 0` and the
  `json.RawMessage` marshal pair. Producer, group, and scheduler
  Register refuse a version below 1 with a plain error.
- Consumer Register sits on `GroupHandle`; `ConsumerHandle` is deleted.
  Producer is `Topic.Producer().Register`.
- Schedules stay system-rooted on `client.Scheduler(name)`; the
  topic-rooted shape was rejected (a redundant topic argument on seven
  of eight verbs and a CLI break).
- Handle type parameter is `Message`; `common.Message[Payload]` keeps
  its name.
- `CompactionHeads` ships in the library with a limit and no CLI
  command. A head read at a mismatched schema version stays as it is
  (ROADMAP Later, beside the upcaster).
- Supersedes the [0619] clause "a struct is generic only when it
  holds Message-typed state": the handle is generic to carry the type
  downward.

Steps, in order:

1. **Docs as the proposal.** `website/src/content/docs/guides/client.mdx`
   carries the typed tree, the key handle, and `RawPayload`, marked
   Proposed where it is ahead of the library. `cmd/vulkan/README.md`
   gains `topic key get <topic> <key>` and
   `topic key messages <topic> <key> --limit`. Review with the user
   before step 2.
2. **Decision record 0646** superseding [0619]'s generic-only-for-state
   clause and recording the settled points above; [0619] status ->
   superseded, linked both ways.
3. **Vocabulary.** `common.RawPayload` with its three methods;
   `pkg/compaction/errors.go` declaring `ErrCompactionHeadNotFound`
   (VK0066, Permanent); the docs page
   `website/src/content/docs/errors/VK0066.md`; link the compaction
   root in tools/conventions and tools/codeexport.
4. **The handle tree in pkg/vulkan.** `TopicHandle[Message]`,
   `KeyHandle[Message]`, `GroupHandle[Message]` with Register,
   `ProducerHandle[Message]`; `ConsumerHandle` deleted; Alert and
   Measurement handles rewritten over `Topic[Alert](...).Key(k)`;
   `CompactionHeads(ctx, limit)` through admin. The version-below-1
   guard on the three Register verbs. The alias closure test in
   tools/conventions decides what else moves.
5. **Call sites.** cmd/vulkan (`Topic[vulkan.RawPayload]`), labs,
   examples/playground, otelvulkan, tools/compat. `go build ./...`,
   `go test -race` on touched packages, directly affected labs.
6. **CLI commands** `topic key get` and `topic key messages`, text and
   `--output json` renderers, README examples verified against real
   output.
7. **Docs pass.** Every page spelling `client.Consumer(` or
   `client.Producer(` moves to the tree: quickstart, replay,
   schema-versions, consumer-group-config, and the client guide's
   Proposed labels come off. VOICE.md revision checklist as its own
   pass.
8. **Close-out.** Full fresh-DB lab suite, HISTORY.md entry, remove
   this section and the ROADMAP Now item it absorbed.
