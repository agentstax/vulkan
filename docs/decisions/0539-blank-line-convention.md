---
status: accepted
date: 2026-08-19
phase: 14b
---

# Blank-line convention for function bodies

## Context

14b's internal file-structure cleanup called for a rule on when a
statement gets a blank line before/after it. Sampling RegisterTopic, the
datastore's registerTopic, and messageRunner.prefetch showed the codebase
already follows one consistent shape -- the rule needed naming, not
inventing.

## Decision

A Blank lines section in CONVENTIONS.md: function bodies read as
paragraphs -- one blank line between steps, none inside a step.

- A step is the group of statements one comment could name; groups you
  would caption separately get a blank line between them.
- Glue rules (mechanical): never a blank line between a statement and what
  consumes its result -- the err check, a nil/comma-ok branch on the
  returned value, the defer that releases what it acquired, the
  Exec/QueryRow running a SQL literal declared above it.
- A mid-body comment binds downward: blank before it, never between it and
  its statement.
- A validation preamble is one step: guards glued, one blank after the
  last.
- At most one consecutive blank inside a body; none directly after `{` or
  before `}`.

Enforcement is the full paragraph rule (user-settled option A), not just
the mechanical glue subset -- the sweep judges step boundaries in every
function body.

## Consequences

- The sweep runs rule-by-rule project-wide (user-settled), shared with the
  file-layout sweep ([0538]): one pass per rule across the main module,
  mechanical rules first, the paragraph-step judgment pass last.
- The comment-binds-downward bullet restates the existing Comments rule
  ("rationale directly above the statement it explains") from the
  whitespace side; the two must stay consistent.
