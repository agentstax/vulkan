---
status: accepted
date: 2026-08-19
phase: 14b
---

# The waterline worker is renamed cursor_advancer; AdvanceWaterline becomes AdvanceCommitted

## Context

"Waterline" was design-round vocabulary that leaked into code: a whole
package (`pkg/worker/waterline`), a persisted worker name, a controller
verb, log lines, and ~150 comments. The fact it names is stored in the
neutrally-named `cursor.committed` column ([0141]), and [0123] already
settled that the codebase's own nouns are the naming authority.

## Decision

- `pkg/worker/waterline` -> `pkg/worker/cursoradvancer`; every symbol
  follows (`CursorAdvancerDefinition/Instance/Controller/Datastore`,
  `WorkerCursorAdvancer = "cursor_advancer"`).
- The verb is `AdvanceCommitted(topicId, groupId) (int64, error)` --
  "committed" is the object advanced, so it stays in the name; the
  two-statement advance and returned committed are unchanged ([0537]).
- `Config.RollRetry` -> `AdvanceRetry`; the instance's tick method is
  `advance`; "roll"/"rollup"/"waterline" wording is rewritten onto
  "committed advances" in comments, labs, CLI help, and the justfile.
- Out of scope: `reference/waterline` (frozen separate module),
  `bench/scale`, doc history, and decision records -- append-only prose
  keeps the old word.

## Consequences

- The persisted `worker.name` value changed; pre-v1 drop+recreate covers
  it, and `workerclaimlab` asserts the new name.
- Alternatives rejected: `committer` (collides conceptually with the
  cursor path's Commit and coins an agent noun); bare `Advance` on the
  controller (drops the object).
