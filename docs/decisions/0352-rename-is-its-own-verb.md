---
status: accepted
date: 2026-07-20
phase: "11.5"
---

# 0352 — Renaming a topic is its own verb, not a NewName field on the alter patch

**Context.** `AlterTopic` and `RenameTopic` both write the topic row, so folding a `NewName` field into `AlterConfig` was the obvious consolidation. But the rename uses the name as its lookup key while alter treats it as fixed identity, and every other table is addressed by id — a rename is metadata-only, a single row update. Dated approximately; built across July 2026.

**Decision.** `RenameTopic` is a separate verb. **Rejected:** a `NewName` field on `AlterConfig` — it would mix identity-change with config-change in one call; one concept gets one named home.

**Consequences.** Each verb keeps a single semantic: alter patches settings, rename changes identity. The CLI mirrors the split (`topic alter` vs `topic rename`).
