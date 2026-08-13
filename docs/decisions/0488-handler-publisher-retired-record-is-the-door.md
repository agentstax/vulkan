---
status: accepted
date: 2026-08-13
phase: "14a"
---

# 0488 — "handler" and "publisher" are retired as domain nouns; the write door is `AlertController.Record`

**Context.** Early drafts of the alert domain named components "handler" and "publisher" — borrowed nouns that describe roles, not what the code literally does in this codebase's vocabulary.

**Decision.** The work a check does is a method on its execution, not a "handler"; the single write door to the alerts topic is `AlertController.Record`, named after the codebase's existing `Record*` verb family rather than "publish".

**Consequences.** The alert domain reads in the same vocabulary as the rest of the codebase, and the one-door rule holds: everything that changes alert state goes through `Record`.
