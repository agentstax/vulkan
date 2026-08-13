---
status: accepted
date: 2026-07-28
phase: "13"
---

# 0365 — Registration is once per instance, and lifecycle sentinels live in pkg/errors

**Context.** A second `Register` call could either silently rebind the instance's lifetime to a new ctx or be refused; and the new lifecycle sentinels were needed by both producer and consumer packages.

**Decision.** A second `Register` returns `ErrAlreadyRegistered`, whether the instance is live ("the first Register's context still owns this producer's shutdown" — silent overwrite would change the lifetime's owner mid-flight) or wound down ("stays down; construct a new MessageProducer"). Revival is a new instance — construction is cheap. Sentinels were hoisted to `pkg/errors` with component-neutral strings ("not registered", "shutdown requested"); wrap sites add "producer for topic X" / "consumer group X on topic Y". Teaching-error templates stay per-package since their snippets name package types.

**Consequences.** Future heartbeat starts and presence-row writes stay strictly single-shot. Same stdlib-shadowing wart as `pkg/context`, same moot-at-root answer.
