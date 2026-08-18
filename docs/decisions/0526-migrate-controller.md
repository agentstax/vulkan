---
status: accepted
date: 2026-08-17
phase: 14b
---

# migrate.Controller is the package's only door; the schema gate reads by id

## Context

pkg/migrate predated the layered pattern: Version / SystemOwner / IsLocked /
AssertSchemaSupported were free functions taking a raw `datastore.Querier`,
so the topic and worker controllers fed them by reaching through
`c.datastore.Datastore.Pool`. Runner and MigrateDatastore had bare
constructor params and no Config structs. The gate took an `*Owner` but only
read id columns, so callers fabricated `NewTopicOwner(systemId, topicId, "")`
— the one call site forcing `Owner.Name` to tolerate `""`.

## Decision

- The four free functions become methods on the package door, renamed
  `Runner` → `Controller`: Controller is the house word for a domain's door;
  Runner stays reserved for run-loop objects (manager.Runner,
  InstanceRunner). `NewController(ds, *ControllerConfig)` +
  `MigrateDatastoreConfig`, both WithDefaults/Validate.
- The datastore keeps conn-taking free funcs (Version, SystemOwner) for the
  advisory-lock-holding flow, plus Wrap-only pool-read method pairs for the
  Controller's reads.
- The gate splits by what callers know: `AssertSystemSchemaSupported(ctx,
  systemId)` / `AssertTopicSchemaSupported(ctx, systemId, topicId)`.
- Every pool read is by id: `SystemVersion(ctx, systemId)` /
  `TopicVersion(ctx, topicId)` — migration_log stores exactly one owner
  column per row, so each read matches its own column and pins the other
  two to NULL. The owner-taking `Version` pair is deleted; the owner form
  survives only as the conn-taking free func, because the locked run flow
  is owner-generic end to end. No `GroupVersion` until group migrations
  exist — nothing writes group rows to migration_log today.
- With no fabrication site left, `NewTopicOwner` / `NewConsumerGroupOwner`
  reject empty names; `Owner.Name` drops its "diagnostics only" caveat.

## Consequences

- No `Querier` in any pkg/migrate public signature; callers (topic/worker
  controllers, systemmanager, admin, CLI) hold a `migrateController`.
- An Owner is never partial anywhere in the codebase.
- SystemOwner stays in migrate: admin imports migrate (rehoming is a cycle),
  and migrate owns the 42P01-before-schemas handling.
