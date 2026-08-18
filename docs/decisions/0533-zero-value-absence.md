---
status: accepted
date: 2026-08-18
phase: 14b
---

# Field absence is the zero value, never a nil pointer

## Context

Read-models mark optional scalar fields with `// "" if unset`
(Message.RoutingKey, CompactionKey). The question was whether to adopt
pointer-typed optional fields (`*string`) repo-wide instead.

## Decision

- Zero value = unset, legal only where the zero can never be real data
  (nobody binds routing key ""; time.Time has IsZero). The field carries
  the `// "" if unset` comment; the datastore's SQL-side NULLIF/COALESCE
  shaping keeps ""<->NULL from diverging.
- Pointer-optionals were rejected: they reverse the standing "outside the
  error path a pointer is never nil" invariant, put a nil guard (or a
  panic) at every read site, and reintroduce the banned nil-safe receivers.
- The ambiguous cases keep their established answers: whole-entity absence
  is a nil struct return from Get ((nil, nil) comma-ok); a tri-state scalar
  widens its domain (ShutdownGrace < 0) or gets a named state; a bool whose
  default would be true takes the inverted Disable* name so zero stays the
  default under WithDefaults.
- The one honest pointer-optional use case is a partial-update (PATCH)
  write shape, where nil = leave unchanged. Vulkan has none; if one
  appears, the pointer rule may apply to that write shape only.

## Consequences

- CONVENTIONS "Pointers & receivers" gains the rule.
- Pointer-typed read-model fields (Message.Options and siblings) must be
  never-nil end to end -- a nilable Options is a bug under this rule.
