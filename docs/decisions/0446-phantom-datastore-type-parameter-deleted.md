---
status: accepted
date: 2026-08-04
phase: "14a"
---

# 0446 — ConsumerDatastore's phantom type parameter deleted

**Context.** `ConsumerDatastore[Message any]` carried a type parameter that appeared in no field, signature, or body — the datastore moves raw payload bytes, and unmarshalling happens above it.

**Decision.** The type parameter was deleted; the datastore is a plain type.

**Consequences.** Callers no longer name a type argument to instantiate a datastore whose behavior never depended on it, and two datastores over the same tables are now the same type instead of distinct instantiations. The signature stops claiming a type-dependence the code does not have.
