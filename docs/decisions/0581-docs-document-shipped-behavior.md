---
status: accepted
date: 2026-08-22
phase: pre-v1
---

# 0581 — the doc site documents shipped behavior only

## Context

The site was written ahead of the library and described a surface that
never existed: `vulkan.Queue`, `Subscribe`, functional options, a shared
`vulkan.events` schema, `FromOffset`, partition keys, replay/redrive
verbs. It also carried performance numbers with no benchmark behind them
(tens of thousands msg/s, a "~50k graduation"), a pitch that each topic's
consumers choose cursor-vs-lifecycle semantics (`ConsumerType`, whose
public surface was deleted 2026-08-19), and a Vulkan Cloud product page.

Documentation drives implementation here: for public-surface work the
page IS the proposal, written and reviewed before the code. A page that
describes an imagined API cannot play that role.

## Decision

Every non-error page documents the real API, with four standing rules:

- Samples compile. Each page's Go is built and vetted in a scratch module
  against the working tree before the page lands.
- A capability that does not ship is labeled Proposed — a sidebar badge
  when the whole page is proposed, an in-page aside when a section is.
  Comparison tables score shipped behavior; a proposed item is never a
  checkmark.
- No performance number without a benchmark record. Unsourced numbers
  were stripped and point at the benchmark pipeline instead.
- CONVENTIONS.md ## Vocabulary governs docs prose exactly as it governs
  code, titles and slugs included: `guides/transactional-enqueue` →
  `transactional-produce`, `concepts/streams` → `concepts/fan-out`.

Two things are retired permanently, not merely rewritten: the
cursor-vs-lifecycle choice pitch (the delivery path is on hold and
`ConsumerType` is deleted — never re-introduce it in docs or marketing),
and Vulkan Cloud, removed site-wide 2026-08-23 with no redirect upkeep
because the site had no users.

## Consequences

- Honest limits became the stronger pitch: retention drops are blocked by
  a lagging group by default (`AllowDropPastCommitted` false), and a new
  group reads retained history because its cursor starts at 0.
- Writing the pages read the code closely enough to surface two real
  bugs, both fixed in the pass: the `RoutingKey` doc comment claimed a
  keyless message reaches every group (it reaches only unbound ones), and
  `deleteSystemTables` omitted `worker_log`, whose FK would have broken
  the drop.
- Startup friction the quickstart exposed feeds DefaultProducer /
  DefaultConsumer; `ProduceInTx` having no value-taking form and the
  absence of a start-from-now option for a new group feed the 14b pass.
- The site now moves ahead of the library only through pages explicitly
  marked Proposed, which double as the specs for that work.
