---
status: accepted
date: 2026-08-18
phase: 14b
---

# consumer/base cleanup: pure constructors, symmetric key verbs, RecordMargin

## Context

NewBaseConsumer did a DB read (GetTopicById) inside the constructor and took
loose timeoutGrace/ackMargin params (deliveryconsumer passed a bare 0 with a
comment); NewBaseDefinition took 6 positional params with trailing
retryPolicy+log; the runner claimed keys through BaseConsumer.ClaimKeyedRun
but released through the exported KeyLeases field; the keylease datastore
public minted its token before Wrap; "ack" is banned vocabulary but AckMargin
was a public config field.

## Decision

- Constructors are wiring, not I/O: BaseDefinition.GetTopic(ctx, topicId)
  resolves the topic (missing topic = wrapped ErrTopicNotFound, not an
  expected absence); NewBaseConsumer takes resolvedTopic -- the
  NewProducerInstance precedent. The ctx param dies with the read.
- The two pacing knobs become BaseConsumerConfig{TimeoutGrace, RecordMargin},
  stored as the exported Config pointer; BaseDefinitionConfig{Logger, Retry}
  replaces NewBaseDefinition's trailing params.
- ReleaseKeyedRun joins ClaimKeyedRun on BaseConsumer; KeyLeases unexports to
  a collaborator field (the runner was its only outside reader).
- NewBaseExecution keeps its [Message] type param (user-settled): inference
  hides it at every call site, and the alternatives widen the surface.
- AckMargin renames to RecordMargin everywhere (Record* is the codebase's
  verb for writing outcomes; QueueMargin set the -Margin suffix). The
  lost-ack comments in append.go/commit.go/multitargetlab now say "commit
  confirmation".
- Wrap-pattern sweep: the keylease token is minted by the CONTROLLER (once,
  before the datastore's retry loop, preserving the retry-stable token
  semantics) and passed as a plain input, so the datastore public is exactly
  Wrap(private) again. migrate RunStep's NoTxn branch moved inside a
  same-named private for the same reason. AppendMessageBatch stays composed
  (heal-and-retry + create-ahead trigger with per-attempt timeouts) -- the
  one sanctioned non-Wrap public, by design not oversight.

## Consequences

- Provision resolves the topic and builds the sparse BaseConsumerConfig;
  deliveryconsumer simply omits RecordMargin instead of passing 0.
- RecordMargin is a breaking config-field rename for callers (pre-v1).
