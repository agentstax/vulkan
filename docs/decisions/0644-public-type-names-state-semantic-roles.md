---
status: accepted
date: 2026-09-03
phase: "pre-v1"
---

# 0644 — Public type names state semantic roles

**Context.** [0625] made handles bare nouns and materialized resources
`*Data`; the later handle split added `Handle` without revisiting the values.
The result mixed representation names (`TopicData`, `MessageData`) with role
names (`TopicSnapshot`, `GroupStatus`, `VersionHealth`), while unqualified
projection names collided as the facade aliased more roots. `WorkerData` also
made it unclear whether `Worker` or `Work` named the domain resource.

**Decision.** An exported type suffix names its semantic role. A lazy identity
plus client is `<Noun>Handle`; the materialized resource or payload is the bare
`<Noun>`. Plural resource reads return materialized values in the existing
list query; singular selectors return no-I/O handles. The remaining roles are
`Config`, verb-led `Options` / `Item` / `Result`, `Snapshot`, `Status`,
`Summary`, `Health`, `Declaration`, `Row`, and `Instance`. `Data` and `Info`
are not roles and are banned exported suffixes. A projection adds its subject
when needed to avoid collisions.

The value renames are Topic, Group, Worker, Schedule, System, and Message[T].
The qualified projections are ScheduleGroupSummary, ScheduleMessageStatus,
TopicVersionHealth, TopicSchemaVersionSnapshot,
ConsumerGroupSchemaVersionLag, and ConsumerGroupLag. Worker stays the noun:
it is Vulkan's maintenance actor and row; Work would name an action or unit.
Alert, Measurement, ProduceItem, ProduceResult, BindingDeclaration,
TopicSnapshot, and ConsumerGroupSnapshot already follow the rule.

Supersedes [0625]'s bare-handle / `*Data` / `List<Child>` naming clauses and
amends [0643]: vulkan declares nine handles and three instance wrappers, no
Producer or Consumer interfaces; aliases keep each declaration's new name.

**Consequences.** The migration is compile-time breaking and JSON is
unchanged. `Client.Topics`, `Client.Schedulers`, `TopicHandle.Groups`,
`SystemHandle.Alerts`, and `SystemHandle.Measurements` delegate to their one
existing list read; the matching singular noun selects a handle. Convention
tests reject exported names ending in Data or Info anywhere under pkg.

Rejected: keeping Data as a generic read-model suffix; Info; returning handles
from plural reads and issuing one Get per handle; `Work` for maintenance rows.
