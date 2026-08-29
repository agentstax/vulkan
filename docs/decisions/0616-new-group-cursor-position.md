---
status: accepted
date: 2026-08-29
phase: pre-v1
---

# 0616 — a new group's cursor position: consumergroup.CursorPosition, Head()

## Context

A new consumer group's cursor row is inserted with every position
column at 0, so on a deep-retention topic every new group reads the
full history before it sees live traffic (playground scenario 07). No
config field, Register option, or admin verb says "start from now".

Peers were surveyed: Kafka auto.offset.reset (earliest/latest/none/
by_duration), kafka-go StartOffset, franz-go ConsumeResetOffset,
Pulsar SubscriptionInitialPosition, JetStream DeliverPolicy +
OptStartSeq/OptStartTime, Redis XGROUP CREATE <id|$>, EventStoreDB
StartFrom (Start{}/End{}/Revision), Event Hubs EventPosition, Kinesis
ShardIteratorType, RabbitMQ x-stream-offset, Pub/Sub (after-create
only, seek is a verb). Two facts held everywhere: the setting is
declaration-time config consulted only when the group has no position
yet, and "latest" means after creation with no fence on in-flight
writes. Two-valued systems use enums; every system that grew id/time
targets is either an enum with companion fields (JetStream, Kinesis)
or a typed position value with a constructor per kind that the
seek/rewind verb takes too (Event Hubs, EventStoreDB, franz-go).

## Decision

- `consumergroup.CursorPosition{Kind}` is the one type every
  cursor-placing surface takes. `CursorPositionKind` is a string enum:
  `""` = beginning (the zero value), `"head"`. Constructors
  `Beginning()` / `Head()` return the value with no error (nothing to
  validate); `AtMessageId` / `AtTime` are named on the doc page but
  ship with the proposed RewindGroup verb, whose sketch now takes this
  type instead of `admin.RewindTo`.
- `consumer.ConsumerConfig.Start consumergroup.CursorPosition`, zero =
  today's behavior. Passed through `RegisterGroup(ctx, topicId, name,
  start)`; used only when the register transaction creates the cursor
  row. An existing group keeps its position.
- Head = `MAX(id)` of the message log inside the register transaction,
  written to `claimed`, `committed`, and `settled_head` via a second
  SQL literal chosen in Go. A produce with a lower id that commits
  after the register is skipped -- Kafka latest / JetStream new -- and
  the page says so; the xid8 fence is not used.
- The "consumer group registered (created)" Info line gains
  `committed` (registry row), so a head start is visible once.

## Consequences

- Shape B (string enum on the config) and A (bool) were rejected: the
  enum grows only by companion fields, the bool not at all; the value
  type gives rewind and Start one home.
- Value semantics: a flat write shape copied into the config, no
  pointer, no nil.
- The sandbox mirror carries the second cursor-insert template;
  consumergrouplab gains the seeded-topic scenario.
