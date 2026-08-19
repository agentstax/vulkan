---
status: accepted
date: 2026-08-19
phase: 14b
---

# Controller and datastore verbs drop their own domain noun

## Context

Most controllers echoed their own noun in every method
(`topicController.GetTopic`, `cronJobController.RegisterCronJob`), and
CONVENTIONS already describes datastore methods as bare resource verbs
("get, register, delete, list"). Some types cannot strip: multi-noun
controllers (WorkerController's Worker + Instance verbs) and the
MessageAdmin facade, where the noun is the point.

## Decision

Strip the noun only when all three hold: it is the type's own domain
noun, the type has a single noun, and nothing collides. Applied to both
layers so the controller -> datastore public -> private mirror keeps one
name per verb.

- Stripped: Topic (Get/GetById/List/Register/Rename/Delete), CronJob
  (Register/Get/List/Suspend/Unsuspend/Delete/ListRequests/Status),
  System (Register/Get/Delete), Compaction (GetHead/ListHeads/
  ListKeyMessages), KeyLease (Claim/Release), CronScheduler (ListDue/
  ClaimDue/Advance/Suspend), ExceptionConsumer (Kill/Claim/RenewLease/
  Record{Success,Failure,Terminal,Superseded} -- now byte-identical to
  DeliveryConsumer's existing bare verbs).
- Unchanged, with the blocking reason: WorkerController (Register would
  collide across Worker/Instance), ConsumerController (nouns are Group/
  Binding, not Consumer), MetricsController (foreign-domain nouns),
  migrate.Controller (System/Topic are dimension qualifiers),
  JanitorController (three SweepExpired targets), ProducerController
  (Message is the object, not an echo), MessageAdmin (facade).

## Consequences

- A same-shaped verb can now exist on two controllers in different
  packages (both cron Suspends); the package qualifier is the subject.
- New methods on stripped controllers must stay bare; reintroducing the
  noun is a regression.
