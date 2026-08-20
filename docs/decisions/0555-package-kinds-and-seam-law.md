---
status: accepted
date: 2026-08-20
phase: pre-v1
---

# 0555 -- Package kinds, the seam law, and worker placement

## Context

The VK0014 raise gap (0554) exposed a structural hole: pkg/consumer's
root is user-facing API coupled to many packages, so the layers beneath
it had no vocabulary home -- unlike topic/system/cron/worker whose roots
are pure vocabulary. An audit found the repo has package kinds it never
named, machines placed by assembler rather than by data, and a door
metaphor ("door") standing in for all of it. Internal components are
full producer-API users (dogfooding is the design), so producer/consumer
sit mid-graph, not on top.

## Decision

Three package kinds; every package is exactly one:

- infrastructure (common, datastore): vocabulary and seams for all.
- domain: pkg/<noun> vocabulary root <- controller <- datastore, plus
  the worker packages maintaining that domain's tables. Every exported
  Err* lives in a domain vocabulary root (or common).
- API package (producer, consumer, admin, systemmanager): constructors,
  configs, instances; assembles domains and workers; declares no
  errors, owns no SQL, holds no vocabulary.

The seam law: anything another stack imports is a seam -- vocabulary
root or domain controller. What only your own tree imports nests
freely (producer keeps controller/datastore/batcher: zero outside
importers). The placement law: a worker package lives under the domain
whose tables it maintains, not under its assembler.

Moves: new pkg/consumergroup domain (vocab = VK0014-16, binding types,
MessageMeta; controller = ex-consumer/controller; workers = base, the
three subconsumers, ex-worker/cursoradvancer); worker/janitor ->
topic/janitor; worker/cronscheduler -> cron/scheduler;
worker/metricscollector -> metrics/collector; admin's VK0008/VK0009 ->
pkg/topic. migrate's root stays: Migration/Validate are declaration
vocabulary, and datastore.Querier in their signatures is an
infrastructure seam -- vocabulary may reference infrastructure.
compaction's fate rides the ROADMAP compaction-API item. "door" is
banned; say what the package literally is.

Rejected: a pkg/message domain for the produce stack -- the whole
library is messages, so it is a junk drawer by construction; vocabulary
leaf under the consumer root (weaker invariant, kept the asymmetry).

## Consequences

~10 package relocations; consumergroup.MessageMeta is the one public
callback-path change. CONVENTIONS.md Package layout rewritten around
the three kinds. The 2026-08-04 consumer-layering record's
read-models-in-controller half is superseded by this shape;
consumer.NewConsumer survives unchanged.
