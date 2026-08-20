# 0563 -- Coded declarations move to pkg/common/diagnostic

status: accepted
date: 2026-08-20
phase: pre-v1

## Context

After [0561]/[0562], common root still held the error anatomy, the log
events, and their shared registry -- three files of one system mixed into
the vocabulary grab-bag, with LogEvent the odd name out beside Error.
[0561]'s "errors stay flat" rested on a `common/errors` package shadowing
stdlib errors; a package named for the concept dodges that entirely.

## Decision

- pkg/common/diagnostic (package `diagnostic`) owns the coded-declaration
  system: registry.go (Declaration, Kind = KindError | KindEvent, the one
  map, register, listRegistered[D], isVKCode), error.go (Error, NewError,
  Recovery, renderers' parts, Errors()), event.go (Event, NewEvent,
  Events()).
- The name: an error and an operator-actionable log event are both
  diagnostics -- coded, explainable, docs-paged. `vulkan explain` explains
  a diagnostic.
- LogEvent renames to Event across the system: NewEvent, Events(),
  KindEvent -- the package qualifier carries the "log" half
  (diagnostic.Event).
- Domain declarations now read diagnostic.NewError / diagnostic.NewEvent;
  errors.go / logs.go file split in domains unchanged.
- common root keeps its own four Err* declarations (errors.go) and the
  retry classification (IsTransientDatastoreError branches on
  diagnostic.Error); imports point strictly downward -- diagnostic imports
  no vulkan package.

## Consequences

- Narrows [0561] again: common root is now vocabulary only; the two
  subpackages are diagnostic (coded declarations) and logging (machinery).
- ~100 qualifier renames across 18 files + CLI + tools walks
  (completeness regex now New(?:Error|Event)).
