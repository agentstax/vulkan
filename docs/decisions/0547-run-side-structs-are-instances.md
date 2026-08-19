---
status: accepted
date: 2026-08-19
phase: 14b
---

# Run-side worker structs are named *Instance; the concrete Definition keeps both roles

## Context

Every worker kind had a `*Definition` struct carrying both `Declare` and
`Provision`, and a `*Execution` struct with `Run`. The roadmap raised a
four-way Definition/Declarer/Provisioner/Instance split because
"Definition and Provisioner are mixed". Survey showed the mixing is
concrete-layer only: the `Declarer`/`Provisioner`/`Execution` interfaces
are already split and consumed asymmetrically on purpose (admin holds
Declarers, systemmanager holds Provisioners). The durable definition is
the `worker` row itself (name, metadata, target_instances); the struct
is the machine that writes and provisions it.

## Decision

- Rename-only: every `*Execution` struct becomes `*Instance`
  (CronScheduler/Janitor/CursorAdvancer/MetricsCollector/Manager/
  PartitionCount/CompactionReadCost/Base), matching the claimed
  `worker_instance` row each holds; files move `execution.go` ->
  `instance.go`; the manager pool follows (`instancePool`,
  `spawnedInstance`).
- `worker.Execution` survives only as the interface name the Instance
  structs implement (settled 2026-08-16).
- No concrete Definition/Provisioner split: a data-only Definition would
  have no consumer -- nothing uses a definition without provisioning it,
  so the split would be a parallel mechanism.

## Consequences

- `i` is now the correct receiver on every run-side struct.
- `ConsumerInstance`/`ProducerInstance` read consistently with the
  worker machinery; no interface named Instance exists, so no seam
  ambiguates.
