---
status: accepted
date: 2026-07-20
phase: "11.5"
---

# 0351 — renameTopic pins the id first and updates WHERE id = $1, never WHERE name = $1

**Context.** `DatastoreRetry` reruns a datastore call on an ambiguous commit outcome — a connection killed mid-commit, where "did it land?" is unknowable. In a rename, the name is the very value being mutated, which makes it the worst possible retry key: if the first attempt did commit, `UPDATE ... WHERE name = oldName` finds zero rows on the retry and the code reports `ErrTopicNotFound` for a rename that actually succeeded. Dated approximately; built across July 2026.

**Decision.** `renameTopic` reads the row first to pin the immutable `id`, then keys the UPDATE on `WHERE id = $1`. A retry re-applies to the same row — setting the name to `newName` again is an idempotent no-op — and returns success. A 23505 unique violation on the new name maps to `ErrTopicNameTaken`.

**Consequences.** Renames are retry-safe under ambiguous commit. Documented hazard: the old name is free immediately, so a stale config can later attach to a different topic registered under it.
