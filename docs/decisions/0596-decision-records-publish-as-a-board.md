---
status: accepted
date: 2026-08-26
phase: pre-v1
---

# 0596 — the decision records publish as a board

## Context

The site counted the records (board-stats read `../docs/decisions` for a
number) but published none; 335 records were repo-only. The board
machinery was typed on the one docs collection end to end, and records
break two site content rules by construction: no frontmatter title (the
H1 is the title) and a body that starts at H1.

## Decision

A Decision records board, the Troubleshooting shape: a generated index
thread at `/decisions/` leads, one thread per record at
`/decisions/NNNN/`.

- A second glob collection reads `../docs/decisions` in place; the
  record number is the collection id. The records stay append-only: the
  site adapts (H1 parsed as the thread title and removed at render),
  vale and remark-lint keep not running over them — the exception is
  recorded in website/CONVENTIONS.md ## Content.
- The board machinery widened from `CollectionEntry<'docs'>` to a
  neutral Thread shape (id, title, description, filePath, entry) both
  collections adapt into (`_board/threads.ts`); board rows, stats,
  prev/next, what's-new, and Pagefind ride along unchanged.
- `[NNNN]` citations linkify through one remark plugin gated to files
  inside docs/decisions; a citation with no matching record stays
  literal (numbers have gaps). Registered as
  `markdown.processor: unified({ remarkPlugins })` — Astro 7 deprecates
  `markdown.remarkPlugins` and coerces it to exactly this.
- The index thread is decisions.mdx plus a record-index component that
  builds its rows from the collection (number · date · status · title,
  newest first) — a computed grid, the thing that earns MDX a
  component; the intro prose stays authored.
- 11 records (0558–0568) carried their metadata as bare lines under the
  H1 instead of the declared frontmatter; the metadata was normalized
  to the format AGENTS.md declares. Decision prose untouched —
  format-defect repair is not an edit of the decision.

## Consequences

- Every record is linkable, searchable, and cross-linked from the
  records that cite it; the board-stats count links to the index.
- threadCount and postCount now count records as threads (417 pages).
- A new record publishes on the next build with no site change; a
  record missing frontmatter fails the build at the schema.
- Edit links are repo-rooted per collection (`website/` prefix for site
  content, `docs/decisions/` for records).
