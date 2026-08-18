---
status: accepted
date: 2026-08-18
phase: 14b
---

# Consumer read-models live with the controller whose verbs return them

## Context

The consumer row stacks (messageconsumer, exceptionconsumer,
deliveryconsumer, base) are worker-kind-shaped: the root holds runner
machinery, then controller, then datastore -- there is no vocabulary root.
Their read-models (Message, ClaimedRange, RangeLease, MessageOutcome,
OutcomeKind, ClaimedException, Delivery, Group, KeyLeaseVerdict,
KeyLeaseClaim) sit in the controllers. Nothing outside a stack imports them
in production; only labs do, and labs import the controller anyway to drive
its verbs. Moving them to the stack root would cycle: the root's runners
import the controller.

## Decision

- A read-model lives with the controller whose verbs return it. A vocabulary
  home is earned by cross-domain consumers, not by the template alone --
  binding.Declaration (admin surface, CLI, consumer instance) is the model
  for that case; Group stays in pkg/consumer/controller because admin uses
  it internally and returns workers/owners, never Group.
- Per-stack vocabulary subpackages and hoisting runners out of the roots
  were rejected: new packages or a three-stack restructure with no importer
  to benefit.
- Ride-alongs: cursor.go's read-models split into files named for them
  (message.go, claimedrange.go, outcome.go); Delivery.Status gets the typed
  DeliveryStatus enum in the deliveryconsumer datastore model (the
  BindingDeclarationStatus pattern), and the delivery table's status column
  gets its value-set comment.

## Consequences

- The three-layer template's vocabulary root stays reserved for domains with
  external read-model consumers; the row stacks are recorded as
  worker-kind-shaped, not incomplete three-layer domains.
- A read-model that later gains a cross-domain consumer moves to a
  vocabulary home at that point, not before.
