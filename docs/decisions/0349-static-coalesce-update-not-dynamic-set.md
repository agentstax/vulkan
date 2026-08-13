---
status: accepted
date: 2026-07-20
phase: "11.5"
---

# 0349 — The alter UPDATE is one static COALESCE($n, col) statement, not a dynamically built SET list

**Context.** With `AlterConfig`'s nil-means-leave-alone semantics, the UPDATE must apply only the non-nil fields. The obvious Go approach builds the SET list dynamically, counting placeholders for whichever fields are present. Dated approximately; built across July 2026.

**Decision.** `updateTopic` is one static `UPDATE ... SET col = COALESCE($n, col)` with the same six-column SET on every call. pgx encodes a nil Go pointer as SQL `NULL`, and `COALESCE(NULL, col)` returns the stored value; a non-nil param is a concrete value — even `0`, which is not NULL — and overwrites.

**Consequences.** The "leave alone" case lives in SQL as NULL instead of in Go as string assembly: the statement reads like every other query in the file, prepares and plans once, and the placeholder-counting closure disappears. The subtlety it gets right for free: an explicit zero (RetentionTTL back to keep-forever) still lands, because COALESCE branches on NULL, never on a zero value. **Rejected:** a dynamically built SET list — per-call string building and placeholder bookkeeping for no semantic gain.
