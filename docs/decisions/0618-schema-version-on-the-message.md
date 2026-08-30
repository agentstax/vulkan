---
status: accepted
date: 2026-08-30
phase: pre-v1
---

# 0618 — the schema version is a message_log column declared by the Message type, not a topic key

Supersedes [0401], [0405], [0408], [0409]; amends [0406].

## Context

`topic.SchemaVersion(1)` appears at every RegisterTopic / Register /
GetTopic call and playground scenario 01 flagged it as the parameter
nobody can explain on sight. Tracing it back: [0401] solved silent
JSON decode drift by making a payload bump a new physical topic, and
[0405] then had to key the catalog `(name, schema_version)` so every
verb could say which physical topic it meant. The premise both
accepted without naming it is that the row carries no description of
its own shape -- the library owns the codec (`NewProducer[Message]`
decodes `payload` into `T`) yet nothing on the row says what `T` the
bytes are. Bytes-first systems (Kafka, SQS, RabbitMQ) never meet the
problem; Confluent's wire format and Temporal's `Payload.metadata`
put the schema identity on the message. Postgres peers that own the
codec (River) rely on JSON's additive compatibility and version
nothing.

## Decision

- `message_log.schema_version BIGINT NOT NULL` -- what this row's
  payload is. `topic_config.schema_version` is deleted; `UNIQUE (name)`.
- The Message type declares its version: `Producer[Message
  topic.Versioned]` / `Consumer[Message topic.Versioned]` where
  `type Versioned interface { SchemaVersion() SchemaVersion }`. Every
  produce writes `Message.SchemaVersion()` into the row. It is the only
  source -- no config override.
- A consumer group claims only rows whose `schema_version` equals its
  Message type's; the predicate joins the binding predicate in
  readMessages and the exception claim. Other versions pass under the
  cursor like a binding miss (skipped, not decoded).
- `RegisterTopic` / `GetTopic` / `AlterTopic` / `DestroyTopic` /
  `RenameTopic` / `Producer.Register` / `Consumer.Register` and the CLI
  `--schema-version` flag drop the version parameter. Log/metric
  `version` attribute now names the row's version where a line is
  about a message, else is absent.
- `FamilyHealth` becomes per-version counts inside one topic; a
  compacted version's retire verdict is now answerable -- heads whose
  row is still the old version, plus unconsumed rows per group -- so
  [0406]'s permanent refusal ends.

## Consequences

- The version is impossible to omit at compile time (missing method),
  which is [0405]'s invariant kept and moved earlier. The struct name
  and its version sit on adjacent lines.
- The bridge [0402] is same-topic: consume v1 rows, re-produce as v2 at
  `CompactionRank -1`. [0403], [0404], [0407] unchanged.
- Cost: [0401]'s per-version isolation (own id space, own retention,
  own partitions, DROP TABLE retirement) is gone; a v1 row lives until
  retention drains it, and retiring v1 means "no v1 rows or heads
  remain", not dropping a family.
- Rejected: a config-level override of the type's version (two sources
  for one fact); an upcaster chain now (ROADMAP Later; the skip
  predicate is where it would plug in).
- Pre-v1: baseline DDL edited in place; stored rows drop and recreate.
