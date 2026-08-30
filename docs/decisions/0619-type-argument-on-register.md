---
status: accepted
date: 2026-08-30
phase: pre-v1
---

# 0619 — the Message type argument sits on Register, not on NewProducer / NewConsumer

Amends [0362].

## Context

`producer.NewProducer[OrderPlacedV1](ds, nil)` names a type at the
constructor, then `Register(ctx, topicName)` names the topic. [0362]
made the constructor take the raw datastore, which is why the type
argument stopped being inferable, and it has been written by hand at
every constructor since. Go's inference reads a call's arguments only,
never a later use, so the argument cannot disappear -- but Go 1.27's
generic methods let it move. The question was where the type belongs:
on the handle that owns a datastore and a config, or on the call that
binds a type to a topic.

## Decision

- `Producer` and `Consumer` are plain structs: `NewProducer(ds, cfg)`
  and `NewConsumer(ds, cfg)` take no type argument.
- `Register` carries it: `func (p *Producer) Register[Message
  topic.Versioned](ctx, topicName)` and `func (c *Consumer)
  Register[Message topic.Versioned](ctx, group, topicName, bindings)`.
- The same rule runs down the producer stack and the compaction reader:
  a struct is generic only when it holds Message-typed state.
  `ProducerController`, `ProducerDatastore`, and `CompactionController`
  hold none, so they are plain structs built once per handle, and
  their methods carry `[Message]` -- `AppendMessage[Message
  topic.Versioned]` reads `topic.SchemaVersionOf[Message]()` itself, so
  the controller's `schemaVersion` field and constructor param are
  gone. `ProducerInstance[Message]`, `Batcher[Message]` (its queue
  holds typed operations), `ConsumerInstance[Message]`,
  `BaseProvisioner[Message]` (holds the handler), `ProducerFunc`, and
  `InTransaction` stay generic.
- A call with no Message-typed argument names the type at the call:
  `heads.ListHeads[alert.Alert](ctx, topicId)`,
  `controller.GetCompactionHeadInTx[Message](...)`.
- Every module's go.mod (and go.work) moves to `go 1.27.0`; the
  quickstart prerequisite reads Go 1.27+.

## Consequences

- One handle per process. The metrics producer and the two alert
  provisioners each held two `Producer[T]` fields for two payload
  types; MessageAdmin held two `CompactionController[T]` fields. Each
  is now one field with per-type method calls.
- The type argument reads beside the topic name -- "this type, to that
  topic" -- and the missing-`SchemaVersion()` compile error lands on
  the Register line, where the topic is.
- Rejected: a zero-value witness argument (`NewProducer(ds, T{}, nil)`)
  to make inference work -- a decoy value in place of a visible type;
  keeping `[T]` on the constructor -- the handle has no per-type state.
