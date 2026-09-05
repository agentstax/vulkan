---
status: accepted
date: 2026-09-05
phase: pre-v1
---

# Alerts take the metrics shape and the one registry

## Context

`SystemHandle.Alert(messageKey)` took the internal
`<name>/<owner-kind>/<owner-id>` key, which only `alert.MessageKey` can
compose from a resolved `common.Owner`; no caller could build it and the
guide's sample passed a string that matched nothing. The three built-in
alerts had no declaration: a name const inside each check controller,
nothing listing them, no `vulkan explain` entry, no docs page. [0647] and
[0648] had just given measurements a declaration catalog, scope handles,
typed selectors, and bare-value `Latest` / `History` reads.

## Decision

- Alerts are declared once, as a fourth diagnostic kind:
  `diagnostic.DiagnosticAlert` (Code, Name, Description, Scope, Severity)
  in the shared VK serial space; declarations live in `pkg/alert/alerts.go`
  and the name consts leave the check controllers. `alert.AlertDefinition`
  and `alert.Definitions(scopes...)` are the defensive view, the twin of
  `metrics.Definitions`. Severity is on the declaration because it is fixed
  per built-in; message, detail, hint, and data vary per finding and stay
  on the value.
- Scope reuses `diagnostic.MetricScope`. Because two kinds now share it the
  type's name wants to be `diagnostic.Scope`; the rename lands in this work
  if its diff stays small, otherwise it is a ROADMAP Later line.
- Three no-I/O scope handles, `System().Alerts()`, `Topic(...).Alerts()`,
  `Group(...).Alerts()`, each with `Definitions()`, `Latest(ctx)` (newest
  retained alert per (name, owner) in the scope, ordered by message key),
  typed selectors for its built-ins (all three are topic-owned today, so
  only the topic handle has any), and the string form `Alert(name)`.
  `Alert(name)` is on every scope, unlike `Metric(name, attributes)`: every
  alert has an owner, so a name on a topic handle has exact topic semantics
  and is where a user-produced alert is read. Selectors are sugar over it.
- One `AlertHandle` holding the alert name and the owner's names, no row.
  `Latest(ctx)` returns the current alert or `(nil, nil)` before the first
  retained message; `History(ctx, limit)` returns retained alerts newest
  first and rejects a non-positive limit. Both return bare `*Alert`;
  `Alert` gains `At`, the observation time, so the envelope can go.
- The message key stays `<name>/<owner-kind>/<owner-id>` and `common.Owner`
  is unchanged. Verbs resolve the owner's names to ids when called, the
  `Group.Get` pattern, then call the one composer. A missing owner surfaces
  as the existing not-found error, never `(nil, nil)`.
- Topic and group `Latest` read the system head list once and keep the
  matching owner inline. No per-owner query.
- Every `MessageKey()` handle accessor is deleted: no caller, and no
  sibling handle exposes its identity.

## Consequences

- `Definitions()` says which alerts Vulkan can raise on a resource before
  one fires; `vulkan explain VK0094`-`VK0096` and three docs pages explain
  a fired one; a pager feed resolves an alert to its declaration by name.
- A destroyed owner's alert reads as not-found rather than as current: the
  checks only evaluate owners still in the catalog, so nothing would ever
  resolve it. The id key also follows a topic through `Rename` and does not
  merge with a later topic reusing the name.
- Rejected: a name-based key and `Owner.TopicName` (splits history on
  rename, merges across recreate); an `AlertOwner` address struct (the tree
  already binds the owner); keeping `StoredMessage[Alert]` for alerts alone.
- `SystemHandle.Alerts(ctx)`, `SystemHandle.Alert(messageKey)`,
  `AlertHandle.Get` / `Messages` are deleted; `admin.ListAlerts` stays as
  internal adaptation. CONVENTIONS ## Package layout adds `alerts.go` to
  the declaring-file set.
