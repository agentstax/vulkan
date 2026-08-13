---
status: accepted
date: 2026-08-01
phase: "13"
---

# 0510 — ConsumerType and its constants are demoted; NewConsumer defaults to cursor consumption

**Context.** `consumer.ConsumerType` with its `CURSOR`/`LIFECYCLE` constants and `ConsumerConfig.Type` made the consumption mode a config enum, exposing an internal mode switch as public vocabulary.

**Decision.** All three are demoted. `consumer.NewConsumer` defaults to cursor consumption; the lifecycle mode is reached via `consumer.NewDeliveryConsumer` directly.

**Consequences.** The constructor chosen, not a config field, says what kind of consumer you get — one fewer enum to document and keep stable, and no invalid mode/constructor combinations to validate.
