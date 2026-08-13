---
status: accepted
date: 2026-08-04
phase: "14a"
---

# 0444 — Consumption-loop packages sit under the door with their own narrow configs

**Context.** Each consumption loop (messageconsumer, exceptionconsumer, deliveryconsumer) uses only a subset of the full ConsumerConfig, so handing every loop the whole struct hides what each one actually depends on.

**Decision.** Each loop is its own package under pkg/consumer, receiving a narrow config carrying only the fields it reads. The door resolves the full ConsumerConfig once and passes each row package its slice.

**Consequences.** A row package's config is an honest statement of its dependencies. The narrow configs repeat field groups across packages — accepted under the standing rule that duplication beats abstraction at this scale.
