---
status: accepted
date: 2026-08-12
phase: "14a"
---

# 0473 — Status and request-listing reads are flat per-fact queries composed in Go, not one CTE statement

**Context.** The first build of the `CronJobStatus`/`CronJobRequests` datastore reads was a single CTE-pyramid statement that welded four independent facts together: which groups match the job name, which messages belong to the job, which is the compaction head, and what each group did. That forced SQL-side machinery — jsonb destructuring, a CROSS JOIN to build the request-by-group matrix, a LEAD window for successor attribution — and duplicated a matching-groups CTE verbatim across two verbs.

**Decision.** The reads were rebuilt as flat per-fact queries — matching groups, message ids, compaction head, and one delivery rollup query per group — composed in Go, where successor attribution is just the slice's previous element and payload fields are unmarshalled by the controller.

**Consequences.** Each query is a dumb resource read on its own tables, shared between both verbs; the composition is a plain loop. Deliberately given up: the reads no longer share one snapshot, so a request produced mid-read can skew a single listing — accepted for a status view, and explicitly not defended with a repeatable-read transaction. **Rejected:** the single CTE statement — four jobs in one query, doing in SQL what plain Go does better.
