---
status: accepted
date: 2026-08-06
phase: "14a"
---

# 0409 — There is no unversioned GetTopic(name); every topic read is version-addressed

**Context.** With multiple schema versions registered under one name, an unversioned lookup raises the question of what it should mean — an error, or "latest."

**Decision.** The overload does not exist. Every read through `pkg/admin` and `pkg/topic` is explicitly version-addressed.

**Consequences.** The "error or latest?" question is resolved by never having to answer it — no default that could silently resolve to the wrong physical topic. Callers that genuinely want to enumerate a family go through the family-level reads (`FamilyHealth`) rather than an implicit resolution rule.
