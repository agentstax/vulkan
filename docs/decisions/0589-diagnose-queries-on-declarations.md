---
status: accepted
date: 2026-08-25
phase: pre-v1
---

# 0589 — a declaration's diagnose part is SQL

## Context

A declaration carried code, recovery, problem, fix (plus consequence on
events). Every part says what to CHANGE; nothing says what to LOOK AT. An
operator reading "message dead-lettered" still has to know which table holds
the delivery and what its columns are called.

Vulkan's state is rows, so the answer is a query -- declarable because
per-topic table names are pure functions of topic_id and the call sites already
log the attributes a query needs. A pasted log line holds every value.

## Decision

`Diagnose(...)` chains onto the declaration, holding an ordered set of
`NewQuery(label, sql)` -- most conditions want "is the row there?" then "what
does its state say?".

- Chained, not a fifth `NewError` parameter: about half the declarations have
  nothing to look at and should not pay a nil. 18 of 53 codes carry queries;
  guards carry none, and absence renders as absence everywhere.
- `Diagnose` follows the WithDefaults pattern -- mutates the receiver, returns
  it -- NEVER the With/Wrap copy pattern. The constructors register the pointer
  at init and every surface reads declarations back from the registry, so a
  copy leaves the registered original bare and renders nothing anywhere. A
  second call panics.
- Placeholders are `{attribute_name}` from the ## Logging attribute registry,
  and the SQL carries the quoting its position needs: bare inside an identifier
  (`delivery_{topic_id}`), quoted per column type in a value position
  (`'{topic}'` text, bare `{group_id}` bigint). psql variables (`:'group_id'`)
  were rejected -- they cannot concatenate inside an identifier.
- The SQL is a const beside the Err*/Event, not a datastore method. ## SQL puts
  all SQL in datastores because datastores EXECUTE it; the library never runs
  this. Metrics declare none, deliberately.

Three surfaces read the one declaration:

- `vulkan explain` renders the queries after the block, never folded into it,
  so the CLI error block stays the tight thing that points here.
- The site reads `website/src/data/codes.json` (`tools/codeexport`), never
  hand-copied prose. The export carries the WHOLE record, not queries alone:
  the error pages' frontmatter was already hand-copied with its drift check
  parked, and the full record made that check a comparison. It also carries
  each query's placeholders, parsed once in Go -- the site's `sqlPlaceholders`
  was deleted rather than left as a second parse.
- The doc comment carries a one-line pointer only. pkg.go.dev renders the whole
  initializer, but gopls hover renders the doc comment and the TYPE, so in the
  IDE the queries are invisible without it.

## Consequences

Paste-your-log-line is unblocked -- it has declared templates to fill, not just
a fix string to interpolate.

The log attribute registry became binding: it stays prose in CONVENTIONS.md,
but a `tools/conventions` walk parses the table and rejects a placeholder or a
raised `With` pair naming something unregistered, which found seven
pre-existing violations. Some raise sites had to start attaching ids they
already knew (topic_id on VK0006/20, group_id on VK0015/16/19) -- a placeholder
is useless if the pasted line never carried the value.

Nothing executes these queries, so no test can prove one runs: the walk checks
placeholders and the `SELECT *` ban, the rest is review. `vulkan explain --run`
stays unbuilt; placeholders named by attribute key keep it reachable.
