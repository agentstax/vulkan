---
status: accepted
date: 2026-07-20
phase: "11.5"
---

# 0356 — No git tag marks this body of work

**Context.** Earlier phases each landed at one clean boundary and got a tag. This work landed incrementally across many commits — the migrations-into-code chunks, then MigrateTopics, then the migrate CLI, then Alter, then Rename, then their CLI verbs — with no single commit representing the whole. Dated approximately; built across July 2026.

**Decision.** No tag was cut: there is no single commit worth pointing one at. The durable record is prose — `ADMIN_CLI.md` and the project's task-tracking entries — rather than a tag.

**Consequences.** Anyone scanning the tag list will find a gap between the neighboring phase tags; that gap is deliberate, not an omission.
