---
status: accepted
date: 2026-08-22
phase: pre-v1
---

# Public read-models carry json struct tags

## Context

The CLI's --output json needs stable wire names for the read-models it
prints, and users embedding read-models (topic.Topic, the metrics
snapshots) in their own HTTP responses or storage otherwise get
Go-spelled field names that break on every rename. The datastore `db:`
rule already treats a field's column name as part of its contract; the
wire name is the same kind of fact. Audit 2026-08-22: no pgtype types
appear in any public read-model (controller adapters only); the only
non-std field type is google/uuid.UUID, which marshals as the
canonical quoted string.

## Decision

Every public read-model field carries a `json:"snake_case"` tag. Keys
spell the log-attr registry's name where the registry has one (topic,
version, group, message_id, cron_job); a field without a registry row
snake_cases its own name (partition_size, created_at). Write shapes,
configs, instances, and controllers carry no tags -- the same boundary
the `db:` rule draws.

## Consequences

- CLI json output and library logs speak one key vocabulary.
- A field rename after v1 is wire-breaking and reviewable as such.
- time.Duration fields marshal as nanosecond integers under
  encoding/json; the CLI renders durations as unit-carrying strings
  through its own result structs instead ([0576]).
