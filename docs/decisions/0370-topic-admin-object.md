---
status: accepted
date: 2026-07-28
phase: "13"
---

# 0370 — Topic administration moves to an admin object holding the datastore

**Context.** `topic.Exists`/`Register`/`Destroy` each took `(ctx, ds, name)`, repeating the datastore on every call — package-level functions with no home for shared config or connection state.

**Decision.** An admin object constructed once with the datastore, so each call needs only `(ctx, name)` and its own arguments. This is the shape that landed as `admin.MessageAdmin` (`admin.NewMessageAdmin`), carrying the topic verbs — `RegisterTopic`, `GetTopic`, `ListTopics`, `AlterTopic`, `RenameTopic`, `DestroyTopic` — plus migration and health reads.

**Consequences.** One construction site owns the datastore and retry/logger config for all administrative calls; the package-level `(ctx, ds, name)` functions stop being the public shape.
