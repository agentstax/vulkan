---
status: accepted
date: 2026-08-19
phase: 14b
---

# worker.Definition becomes data; the concrete machines are *Provisioner

## Context

[0547] rejected a concrete Definition/Provisioner split for lacking a
consumer. The user reopened it: code should match the mental model — a
Provisioner provisions a Definition. Survey found the consumer [0547]
missed: 8 of 10 kinds' Declare bodies were the identical
ValidateOwner+RegisterWorker boilerplate over four data points, and the
manager already hands Provision the declared row in pieces.

## Decision

Narrows [0547]: the *Instance rename stands; the no-split clause flips.

- `worker.Definition` is a data struct — Name, Metadata, OwnerKind
  ("" = any kind may declare one; the manager's case), TargetInstances
  (0 -> 1). The lifecycle reads: Definition -> declared as a worker row ->
  provisioned into an Instance -> Run.
- `WorkerController.DeclareWorker(definition, owner)` is the one row-write
  verb: owner-kind guard + RegisterWorker. Every kind's Declare ends here.
- Concrete `*XDefinition` machines are renamed `*XProvisioner`; each builds
  its Definition in its constructor and stores it. `Provisioner` interface:
  `Definition() *Definition` (replaces Name) and
  `Provision(ctx, declared *worker.Worker)` (replaces the id/owner/metadata
  triple — the row IS the declared definition).
- `Declarer` stays universal — the 8 data kinds' Declare is the one-liner
  through DeclareWorker (consumers inherit it from BaseProvisioner, which
  stamps NoInstanceTarget as a base invariant); the alerts keep their
  behavioral preamble (group + binding + owner mint) ending in the same
  verb. The Declarer+Provisioner bundle interface is deleted.
- Declare returns only error: provisioning always reads the row fresh
  (newest declaration wins); a returned row would be a stale second carrier.

## Consequences

- Labs that overrode metadata at Provision now set row.Metadata before the
  call. K8s spec-vs-object is the precedent for the Definition/Worker pair.
- The alerts' Declare preamble is them hand-rolling consumer registration —
  a candidate post-v1 cleanup, out of scope here.
