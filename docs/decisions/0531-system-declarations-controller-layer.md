---
status: accepted
date: 2026-08-18
phase: 14b
---

# System-topic and cron-job declarations live in the domain's controller

## Context

Three vocabulary packages imported controller layers upward: cron, metrics,
and alert each declared TopicConfig() *topiccontroller.TopicConfig beside
their TopicName const; alert's vocabulary also held Job (storing a
*croncontroller.CronJobConfig), NewJob, and the ToJobData adapter.
Vocabulary imports ~common only, and adapters are controller-layer.
The declarations' only production consumer is admin.RegisterSystem; the
kind packages (partitioncount, compactionreadcost) build Jobs and decode
JobData in executions.

## Decision

- A domain's system-topic declaration (TopicConfig) lives in its controller
  package (system_topic.go): the declaration is domain knowledge, but its
  type is controller-layer, so the controller is the layer that can legally
  speak it.
- TopicName consts stay in vocabulary -- pure strings with many consumers.
- alert Job, NewJob, and ToJobData move to pkg/alert/controller (job.go,
  adapter.go); JobData stays vocabulary.
- Inlining the declarations into admin was rejected: it scatters domain
  facts into the composer, reservedtopiclab asserts against the declaration,
  and kinds cannot import admin (cycle).

## Consequences

- cron, metrics, and alert vocabularies import ~common (+ own domain) only;
  their topic/topiccontroller/croncontroller imports are gone.
- Admin and the labs call metricscontroller/alertcontroller/croncontroller
  .TopicConfig(); kind job.go and execution.go use alertcontroller.Job /
  NewJob / ToJobData (they already imported alertcontroller).
- topic and system declare no system topics, so they need nothing.
