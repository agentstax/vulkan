---
status: accepted
date: 2026-08-04
phase: "14a"
---

# 0445 — MessageMeta lives in the one new leaf, pkg/consumer/message

**Context.** The consumption-loop packages stamp MessageMeta into ctx under an unexported key, and the user reads it back inside their consumerFunc via MetaFromContext. Exactly this symbol failed the "nothing a user types lives below the door" sort: users type it, but the rows below the door must also reach it.

**Decision.** One new leaf package, pkg/consumer/message, holds MessageMeta and MetaFromContext — the single package every row imports, sitting below the rows and still user-reachable.

**Consequences.** The ctx key exists exactly once, so stamp and lookup always agree. **Rejected:** duplicating the type per row package — each copy's unexported key would be a different value, and MetaFromContext would return false for meta stamped by another package.
