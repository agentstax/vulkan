---
status: accepted
date: 2026-08-28
phase: pre-v1
---

# 0610 — example attribute values on code threads

## Context

[0590] shipped the paste box and noted its gap: the composed example
line at the top of a code thread carried no attribute values, so a
reader with no log of their own had nothing that demonstrates the
fill. The ROADMAP item promoting values from cosmetic to load-bearing
needed a home for the values and a composition faithful to the lines
the library actually emits.

## Decision

A shared site-side value table, not per-page frontmatter and not a
registry export: `exampleValues` in src/pages/_thread/example-line.ts
maps each log-attribute-registry name to one value, so every thread
demonstrates against the same fictional deployment (topic
orders.created id 1, group id 7, message 214; library-real names —
topic_janitor, alert.partition_count, the 1,000,000 default partition
size). A per-code override map handles declarations whose values must
contradict the shared row (VK0023 mirrors VK0022, so its
version/build_version flip). A name with no table row throws at build.

Composition mirrors the real renderers: errors get the Error()
one-liner — problem, `name value` pairs, the fix FILLED from the same
values, code — quoting strings and leaving digit-leading values bare,
exactly what the paste parser's strict one-liner pattern reads; events
get the text-handler line with bare `name=value` attributes. A code
with no placeholder names keeps the minimal line.

The invariant is a test, not a hope: example-line.test.ts composes
every placeholder-carrying code's line from codes.json and asserts
logAttributes fills every one of its own paste placeholders.

The line carries ALL of a code's fillable names (VK0005 shows topic
and topic_id together though a real raise attaches one) — maximal
demonstration chosen over per-raise realism; trimming would need
per-page authoring, deferred with the frontmatter rung.

## Consequences

LogLine's blank-marking render path became unreachable (every
placeholder now fills) and was deleted along with the
markPlaceholders helper; the component is a plain pre. Per-page
frontmatter overrides remain the next rung if a page's prose ever
wants its own values.
