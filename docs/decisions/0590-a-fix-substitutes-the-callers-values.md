---
status: accepted
date: 2026-08-26
phase: pre-v1
---

# 0590 — a fix substitutes the caller's values

## Context

[0589] gave diagnose queries `{attribute_name}` placeholders and exported them,
so a code thread could fill a query from a pasted log line. The fix could not
be filled: it is one string fixed at declaration, and the ## Errors rule that a
fix names "the exact field, method, or command with the caller's real values
interpolated" had no mechanism behind it. Every fix was static prose.

## Decision

A fix may carry the same `{attribute}` placeholders, filled from the values the
raise attached. One vocabulary, one parse, three fillers.

- `placeholder.go` is the vocabulary's one home -- the pattern, `placeholderNames`,
  `fillPlaceholders`. `Query.Placeholders` and `Error.FixPlaceholders` both read
  it; the second parse that would have appeared in error.go does not exist.
- `Error()` and `LogValue()` fill through `Error.Fill`, so the line an operator
  reads already names their own topic, group, or version. `Fill` is exported
  because the CLI rewrites some fixes into vulkan commands (`cliFixes`) and
  fills its own wording the same way -- that is what makes "runs verbatim as
  pasted" true rather than aspirational.
- The value goes in RAW, never through `formatValue`'s quoting: the surrounding
  text carries the quoting its position needs, exactly as a query's SQL does
  (`register "{cron_job}"` in prose, `'{topic}'` in SQL).
- An unfilled placeholder renders literally. A visible blank cannot be mistaken
  for a value, and a `tools/conventions` walk over every `return <declared Err>`
  is what keeps one from reaching a reader.

**A fix placeholder must be attachable at EVERY raise site.** A fix is one
string for all of them, so a name one site cannot supply is a blank on a real
operator's line. The walk enforces it; writing the rewordings is what found it.
Diagnose queries are deliberately exempt -- a declaration carries an ordered
SET of them, so one keyed on a name and another keyed on an id let whichever
value the line carries find something to run.

Six fixes name a value: VK0004, VK0013, VK0022, VK0023 took one. VK0005 and
VK0014 were reverted mid-build: three VK0005 sites and one VK0014 site resolve
by id and the name is genuinely unknowable there (`Owner.Name` is the owner
attribute, not the topic). They gained an id-keyed query instead, which also
closes the [0589] gap where those same sites raised a condition whose query
named a value the line never carried.

The site fills from the pasted line. `logAttributes` looks each placeholder up
BY NAME rather than parsing the line's grammar, trying the three shapes the
library emits -- text handler, JSON, the `Error()` one-liner. The one-liner
pattern is the strict one: a problem line repeats an attribute's own name, so
`topic not found` must not read "not" as the topic, and only a quoted string or
a digit-leading value counts. `fillSegments` decides how a value enters SQL from
the quoting already around the blank: quoted position doubles any `'`, bare
position accepts only `[A-Za-z0-9_]` and otherwise stays a blank rather than
render SQL that cannot run.

## Consequences

The paste box replaces the search strip under each code thread, and the queries,
the fix, and the copy button all read one `$state` module — the same cross-island
pattern `read-tracking` already ships.

The composed example line carries no attribute values, so pasting it fills
nothing; the ROADMAP's per-page example-values item is now what makes the
feature self-demonstrating. The `LogLine` at the top of a thread marks the fix's
blanks rather than printing them as literal text.

`vulkan explain` renders fixes unfilled, which is correct: it documents the
declaration, and a template reads as one.
