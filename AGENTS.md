# Agent instructions

How to work in this repo.

**REQUIRED: `conventions.md` (repo root) governs all code** -- dependencies,
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

## Docs & record-keeping

- The record-keeping surface is fixed: TODO.md (small sliding window of
  tasks), LEARNING_PLAN.md (roadmap, settled designs, done-records),
  conventions.md (code rules), AGENTS.md (this file). Never create new doc
  files or ADR directories for decisions -- fold them in.
- Planning/review docs the user asked for go in repo root where they can read
  them; they get deleted at close-out once folded into LEARNING_PLAN.md.
- "Explain it back" sections in LEARNING_PLAN.md/NOTES.md are the user's
  learning exercise: write only the numbered questions, never the answers,
  unless the user explicitly dictates or asks for edits to their own text.
