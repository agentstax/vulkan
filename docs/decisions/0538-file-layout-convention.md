---
status: accepted
date: 2026-08-19
phase: 14b
---

# File content ordering convention

## Context

14b's internal file-structure cleanup called for a file content ordering
rule. The only ordering rule on the books was datastore-specific
(pair-by-pair, with deeper helpers following the pair that uses them);
controllers, runners, and config files had no stated order, and "helper"
had no definition.

## Decision

A File layout section in CONVENTIONS.md, applying to every file in the
main module:

- Top of file holds only the file's free vars/consts; a const or var block
  owned by a type (enum values, a type's sentinels) stays glued to its
  type, never hoisted.
- Then each type's block: struct, New<Struct>, WithDefaults/Validate.
- Then methods. Files with exported methods order pair-by-pair (public
  immediately followed by its same-named private). Files with no exported
  funcs order by lifecycle: entry point first, then each step in the order
  the running code reaches it.
- A helper is a package-level non-receiver func in a file that otherwise
  holds methods (user-settled definition -- mechanical, no judgment call).
  All helpers go at the bottom behind one banner:

      // ***************
      // *** HELPERS ***
      // ***************

- Methods are never helpers. A file of only free funcs (an adapter.go) has
  no banner.

## Consequences

- The Datastores ordering bullet narrows: deeper helper METHODS still
  follow their pair; free funcs move to the bottom helper block.
- The codebase-wide sweep is pending, run rule-by-rule project-wide
  (user-settled) together with the blank-line convention sweep (ROADMAP).
- Labs are out of scope -- narrative mains keep their own shape.
