---
status: accepted
date: 2026-07-10
phase: "8b"
---

# 0244 — Topic identity is a per-call datastore parameter, not a field on the datastore

**Context.** Once datastore queries became topic-scoped, topic identity had
to reach them somehow. It was first threaded in as a `Topic *topic.Topic`
field on the consumer/producer datastore structs, set once at construction —
then deliberately reversed mid-build.

**Decision.** Every `Datastore[Message]` method that needs a topic-scoped
table name or a `WHERE topic_id = $N` takes a `topicID int64` parameter,
placed right after `ctx` — mirroring exactly how `consumerGroup` is already
passed on every call despite also being fixed for a `WorkConsumer`'s whole
lifetime. `WorkConsumer`/`WorkProducer` keep their own `Topic` field
(legitimately fixed per instance, same as `Group`) and pass `p.Topic.Id` at
each call site. Constructors were renamed `NewPostgresDatastore` →
`NewConsumerDatastore`/`NewProducerDatastore`.

**Consequences.** One datastore instance can correctly serve multiple
topics, which the field-based design structurally could not. The datastore
stays stateless about domain identity; scope is visible in every call
signature.
**Rejected:** `Topic` field on the datastore struct — binds an instance to
one topic and diverges from the existing `consumerGroup` per-call pattern.
