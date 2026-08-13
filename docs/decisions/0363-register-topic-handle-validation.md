---
status: accepted
date: 2026-07-28
phase: "13"
---

# 0363 — Register validates the topic handle fail-fast: by-name fetch plus whole-struct compare

**Context.** A producer holds a `*topic.Topic` handle that can go stale: the topic row can be deleted, destroyed-and-recreated (the handle then addresses dropped tables), or altered. Failing at Register beats failing at first Produce.

**Decision.** Register does a name-keyed `GetTopic` and compares the whole struct against the held handle. Three distinct outcomes: row gone returns `topic.ErrTopicNotFound`; id changed returns `topic.ErrTopicStale` (destroyed and recreated — the id rides in the struct, so the by-name fetch catches it); any other field drifted also returns `ErrTopicStale`. Whole-struct compare settled the "which fields matter" question: a stale handle is a stale handle.

**Consequences.** No partition cold-start work at Register: the producer's `23514` self-heal is synchronous with the insert that needs it, so a Register-time ensure would only shave one round-trip once per process. Revisit with evidence — a producer-only topic pays the heal at every partition boundary.
