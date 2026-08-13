---
status: accepted
date: 2026-07-03
phase: "7"
---

# 0206 — Matching is on routing_key only; no header or content matcher

**Context.** Routing could have matched on message headers or payload
content (JSONB containment) as well as on the topic-style `routing_key`.

**Decision.** Only `routing_key` matching against `binding.pattern` was
built. The hard part of routing is wiring one predicate into both consume
models; a single matcher style is enough to do that.

**Consequences.** Header/content matching is cut, not abandoned — it remains
a documented optional follow-up. Keeping one matcher style is also what made
the `binding` schema collapse (see 0207).
**Rejected (for now):** JSONB containment header/content matching — extra
matcher machinery with no lesson in it beyond what `routing_key` already
teaches.
