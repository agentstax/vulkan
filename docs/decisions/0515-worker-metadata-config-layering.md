---
status: accepted
date: 2026-08-15
phase: "14"
---

# 0515 — Group tunables live in worker metadata as {default, override}; users tune through domain nouns

**Context.** Consumer tuning (tick rate etc.) lived only in ConsumerConfig --
per-binary, no live tuning. Worker rows already carry metadata JSONB parsed at
claim time (janitor/manager/cronscheduler poll_rate), but altering it meant
raw SQL, and "worker" is an internal concept most users should never meet.
Research: RabbitMQ built policies because immutable declaration args were
operationally miserable; K8s server-side apply settled on field ownership so
code and operators never fight over one value; Kafka's describe shows every
effective value with its source.

**Decision.**
- Every tunable worker-metadata key is nested: `{"default": v, "override": v}`.
  Effective = override, else default. Consumer Register writes `default`
  unconditionally (code is truth for its layer); alter verbs write `override`,
  which survives deploys until explicitly cleared. No conflict semantics
  needed -- the layers are different facts.
- Group-tunable ConsumerConfig fields (each consumer kind gets its own
  metadata struct): ClaimPollRate, MaxRangeReclaims, ExceptionInitialBackoff,
  Message defaults, ConcurrencyOverride. Message overrides clamp into the
  code-owned MessageMin/MessageMax bounds via the existing machinery, warn on
  clamp. Everything else (BatchLimit, QueueSize, MessageConcurrency, margins,
  TTLs, shutdown, Retry, Logger) stays instance-local in ConsumerConfig.
- Surface tiering:
  - App devs: ConsumerConfig in code -> default layer. Never see workers.
  - Operators: admin.AlterGroup + `vulkan group alter` (override layer,
    explicit --clear) and `vulkan group get` showing effective value + source.
  - Powerusers: `vulkan worker` tree -- list / get / alter / suspend / resume
    -- addressing any worker row (janitor, manager, cronscheduler, alert
    consumers) by owner + name; alter writes the same override layer and
    target_instances. No worker register/destroy: rows are declared and
    cascade-deleted by their owning domains.
- Existing worker kinds' flat metadata ({"poll_rate": ns}) reshapes to the
  nested form -- one scheme everywhere.
- Changes take effect at the next claim life, like every worker metadata read.

**Consequences.** Live per-group tuning without redeploys; operator overrides
survive deploys instead of silently reverting (the K8s HPA-vs-CI/CD failure).
Drift between code and override is visible in group get, never fought over.
Rejected: register-vs-alter mismatch errors (RabbitMQ's escape from exactly
that), last-writer-wins (the silent-revert outage pattern).
