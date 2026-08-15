---
status: accepted
date: 2026-08-15
phase: "14"
---

# 0514 — DestroySystem is RegisterSystem's inverse: drop every topic, then the control-plane schema

**Context.** Topics have DestroyTopic, groups have DestroyGroup (2026-08-13);
the system row and the shared control-plane tables RegisterSystem creates had
no destroy verb. A partial form (delete the system row, keep user topics) is
incoherent -- every topic rides on the shared schema, so the only meaningful
scope is full teardown.

**Decision.**
- `admin.DestroySystem(ctx, opts DestroyOptions)`, gated on AllowDestroy like
  the other two verbs. It deletes every registered topic through the existing
  `topicController.DeleteTopic` path (keeping its partition-drain safety
  against a still-writing producer), then `systemController.DeleteSystem`
  drops the shared tables in one transaction under `common.AdvisoryLock` --
  the same lock RegisterSystem serializes on. The database returns to its
  pre-RegisterSystem state.
- Guards, unless opts.Force (checked before any work):
  - any live worker instance (a manager or consumer still heartbeating)
    -> `system.ErrSystemLive`
  - any non-`__system.` topic still registered -> `system.ErrTopicsRegistered`
- Force destroys user topics and their messages too -- mirrors DestroyTopic's
  Force deleting messages; the guard plus the CLI warning make the loss
  explicit.
- Idempotent, mirroring RegisterSystem: an already-destroyed (or never
  registered) system resolves as a no-op, and a re-run after a partial
  failure resumes where it stopped. The CLI still tells an operator "nothing
  to destroy" from its own pre-flight.
- CLI: `vulkan system destroy` with --force/--yes on the system tree. The
  confirmation phrase is the connected database's name (`current_database()`)
  -- the system has no name of its own, and typing the database name proves
  the operator knows which database they are pointed at.

**Consequences.** RegisterSystem stands everything back up after a destroy.
The guard sentinels live in pkg/system (vocabulary), beside
ErrSystemConfigMismatch. A producer or consumer running through a forced
destroy fails on its next statement once its tables vanish.
