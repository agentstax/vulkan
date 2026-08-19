---
status: accepted
date: 2026-08-19
phase: 14b
---

# Bare sub-consumer constructors: doc fencing, not structural fencing

## Context

The worker-tier review re-examined the planted trap: the sub-consumer
definition constructors kept their public shape while their meaning changed
(bundle -> bare work loop). The accident is two lines --
NewMetricsProducer(ds, nil) + NewMessageConsumerDefinition(...) -- and a
bare message consumer runs convincingly while writing exception rows
nothing retries and never advancing the waterline: silently dead retries
and pinned retention. The 2026-08-01 trim decision keeps all three
constructors public (custom fleets compose definitions into
manager.NewManagerDefinition), so deletion or unexporting was off the
table.

## Decision

Fence with documentation, not structure:

- Each sub-consumer package gets a package doc stating it is ONE worker
  row of the assembled consumer group, naming what running it alone does
  NOT do, and pointing at consumer.NewConsumer as the assembled path.
- Each constructor doc opens with "builds one worker row of the group, not
  the assembled consumer -- see the package doc."
- consumer/base's package doc states it assembles nothing.
- deliveryconsumer's ON HOLD package doc gains the same one-row line.

No signature change: restructuring (e.g. constructors taking a
*BaseDefinition) would churn a surface the config-refinement and naming
passes re-touch later in 14b, for friction docs already provide.

## Consequences

- The runtime backstop stands and is named in the messageconsumer doc: a
  group running only the message consumer accumulates unresolved
  exceptions, which the default unresolved-exceptions alert surfaces.
- If the later passes reshape these constructors, the package docs carry
  the fencing unchanged.
