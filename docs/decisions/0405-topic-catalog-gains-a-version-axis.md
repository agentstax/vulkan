---
status: superseded
date: 2026-08-06
phase: "14a"
---

# 0405 — The topic catalog is keyed (name, schema_version), and constructors require SchemaVersion positionally

**Context.** With a schema version bump being a new physical topic under the same name, the catalog must hold multiple registered versions of a name and every code path must say which one it means.

**Decision.** `topic (name, schema_version)` UNIQUE together. `RegisterTopic`/`GetTopic`/`AlterTopic`/`DestroyTopic` are all version-addressed; `RenameTopic` moves every version registered under a name. `NewProducer`/`NewConsumer` (and the split consumers) take `topic.SchemaVersion` as a required, positional, typed constructor parameter, and `vulkan topic register/get` grow a `--schema-version` dimension.

**Consequences.** Version is impossible to omit — there is no default that could silently bind the wrong physical topic. Each version has its own log, id space, and duties; the name is a family label, and rename is the only verb that operates on the whole family.

Superseded by [0618]: the schema version is a message_log column declared by the Message type; the topic catalog is keyed by name alone.
