---
status: accepted
date: 2026-07-03
phase: "7"
---

# 0204 — BindTopic and ClearBindings are admin calls, not Datastore interface methods

**Context.** Managing `binding` rows is setup/operations work, not something
the produce or consume hot paths ever do.

**Decision.** `BindTopic` and `ClearBindings` live in
`pkg/consumer/bindings.go` as admin calls and are deliberately kept off the
`Datastore` interface.

**Consequences.** The interface stays scoped to the runtime produce/consume
verbs; binding administration is a separate, explicit surface that runtime
code cannot accidentally reach for.
