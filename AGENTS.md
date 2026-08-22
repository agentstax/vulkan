# Agent instructions

How to work in this repo.

**REQUIRED: `CONVENTIONS.md` (repo root) governs all code** -- dependencies,
naming, structure, package layout, datastores, constructors/configs,
migrations, SQL, comments. Violations there are bugs, not style nits. Read it
before writing or reviewing code unless it is already in context -- the root
`CLAUDE.md` imports both files, so Claude Code loads them at launch. This file
covers session workflow only; the two are a set.

## Responses

- Answer the question in the first line, then <=4 bullets of load-bearing
  facts. No prose walls. Deep-dives only on explicit request.
- When asked how something actually happens, switch registers: numbered
  causal steps with the real code/SQL inline at the step it belongs to, plus
  one worked concrete example (named group, real ids). Short means cutting
  topics, not compressing a mechanism into fragments.

## Design process

- Ad-hoc helpers, resolution logic leaking into SQL, cap/patch-up steps after
  the main computation, or two helpers computing flavors of the same concept
  are STOP signals -- the design has a hole. Name the broken premise, research
  how established systems (K8s, Kafka, SQS, Temporal, RabbitMQ) solve it, and
  propose BEFORE writing code. Working-code-that-passes-tests is not the bar.
- Trace consequences user-side before proposing: silent behavior changes need
  an observability answer, not a docs answer.
- Plan wording about mechanisms is intent, not implementation mandate --
  satisfy the invariant with the smallest delta to existing code.

## Verification

- Per change: foreground targeted checks only -- build, `go test -race` on
  touched packages, directly-affected labs.
- Full fresh-DB lab suite only at review-ready checkpoints or on request,
  never background-per-change.

## Releases

At a release checkpoint, after the full fresh-DB suite:

- Run `just compat-lab` with tools/compat pinned to the prior tag (pin
  flow in its go.mod comment), passing the verdict the migration registry
  declares.
- Update the compatibility table in
  website/src/content/docs/guides/migrations.mdx.
- Cite the lab outcome in the release's HISTORY.md entry.

## Docs & record-keeping

The record-keeping surface is fixed -- never create doc files outside it.
Working docs live under docs/; only the rule files (CONVENTIONS.md, this
file) and README/CLAUDE.md stay at root:

- docs/TODO.md -- sliding window of in-flight work ONLY.
- docs/ROADMAP.md -- future work: Now / Next / Later / Parking lot. Reorder
  by moving items; an item accumulates design notes as sub-bullets in place.
  New ideas land in Later or the parking lot, never in TODO.md.
- docs/HISTORY.md -- dated done-ledger, newest first, one entry per shipped
  milestone, citing decision records as [NNNN].
- docs/DECISIONS.md -- the index: one line per record, the retrieval layer.
  Grep it first, open only what's needed. Record bodies live in
  docs/decisions/ (NNNN-<slug>.md, front matter status/date/phase,
  Context/Decision/Consequences, under 60 lines).
  Records are append-only and written in the SAME session a design settles;
  changing a decision means a new record plus flipping the old one's status
  to superseded, linked both ways. A new record takes the next number after
  the current max.
- CONVENTIONS.md (code rules) and AGENTS.md (this file) hold the binding
  CURRENT rules -- never infer today's rules by replaying decision history.

Lifecycle of a piece of work: idea -> ROADMAP (Later/parking lot) ->
promoted to Now -> expanded in TODO.md when picked up -> design settles ->
decision record -> ships -> HISTORY.md entry; its TODO.md and ROADMAP.md
lines are removed.

- Planning/review docs the user asked for go in repo root where they can
  read them; deleted at close-out once folded into the surface above.
- docs/archive/explain-it-back.md is the user's own writing (archived from
  the deleted LEARNING_PLAN.md/NOTES.md) -- never edit it. Some decision
  rationale exists only there; it is source material, not disposable.
