---
status: accepted
date: 2026-07-20
phase: "11.5"
---

# 0347 — Missing or mismatched schema surfaces as teaching errors, not raw Postgres errors

**Context.** A caller who skips bootstrap, or runs a binary against a database migrated past (or short of) what it was compiled for, would otherwise hit a raw 42P01 (undefined table) from deep inside some query — accurate but useless for diagnosis. Dated approximately; built across July 2026.

**Decision.** `RegisterTopic` gates on the system being registered, raising an error that wraps `migrate.ErrNotRegistered` instead of leaking 42P01. Lifecycle `AssertSchemaSupported` fails a producer/consumer fast when the database's schema version is outside the range the binary compiled against. The CLI's `translateAdminError` turns a raw 42P01 into "run `vulkan migrate init` first."

**Consequences.** Every schema-state failure is caught at a named gate with a wrapped sentinel a caller can match on, and the failure names its remedy. The cost is a pre-flight check on registration and lifecycle startup paths.
