---
status: accepted
date: 2026-08-25
phase: pre-v1
---

# 0588 — the compatibility matrix is exported from the gate's own rule

## Context

[0580] settled MinCompatibleVersion per migration step and the floor gate that
admits a build iff `min_compatible <= build <= current`. The shape that rule
produces -- a rolling-deploy window widening with additive steps and slamming
shut on a breaking one -- is easier to see as a grid than to read as prose.

A grid could be hand-written or computed in TS from an exported registry. Both
put a second copy of the gate where it can drift from the one binaries
enforce. Both registries are also empty pre-v1, so whatever is built has one
cell of real input to prove itself against.

## Decision

Go decides every verdict; the site does a table lookup.

- `migrate.ClassifySchemaSupport(version, minCompatible, build)` returns a
  three-value enum and is the single home of the rule.
  `assertVersionSupported` is now a switch rendering that enum as the declared
  error. `migrate.Version(registry)` absorbed the `len(Registry)+1` formula
  both scope registries had written out.
- `tools/compatexport` walks build x database and calls the rule per cell,
  writing `website/src/data/compat.json`. It takes registries as PARAMETERS,
  never reading the package-level ones -- that is what makes versions that do
  not exist testable, the same move `migration_test.go` already makes against
  synthetic registries and `schemagatelab` makes by forging migration_log rows.
- The export carries verdicts, not steps to evaluate: TS never compares a
  version to a floor. `just site-verify` regenerates and `git diff
  --exit-code`s the JSON, so skipping regeneration fails the build.
- The component renders with no `client:` directive: static HTML, zero JS.
- The corner cell is EMPTY and each header names its own axis (`database v3`,
  `build v3`). Researched against real matrices -- Cluster API, IBM Content
  Manager, Oracle interoperability -- which either empty the corner or use the
  spreadsheet `row \ column` slash. Two stacked arrow labels and a two-row
  `rowspan`/`colspan` header were both built and rejected on sight.

Both registries are empty, so the shipped grid is one cell. A fixture registry
(v2, v3 additive; v4 breaking; v5 additive) lives in
`tools/compatexport/example.go` behind `-example`, is what the export's tests
assert against as a whole-grid text literal, and is REVIEW SCAFFOLDING to be
deleted with its flag once the shape is settled.

## Consequences

The page can never disagree with the library about compatibility: one rule,
one caller, a generated artifact between them.

The export models clean upgrade paths only. The gate reads the recorded
migration_log (`latest-by-id, not MAX` -- a downgrade records a lower version),
while the export derives each row's floor from declared steps at or below it.
Different questions, not a second read path -- but a rolled-back database can
sit at a version whose real floor the grid never draws. The page says so.

Storybook fixtures are pasted export output -- Storybook cannot run Go and TS
must not compute a verdict -- the one hand-copy here.

`just site-verify` FAILS on the drift check while the fixture is committed.
That is the point -- the fixture cannot ship by accident.
