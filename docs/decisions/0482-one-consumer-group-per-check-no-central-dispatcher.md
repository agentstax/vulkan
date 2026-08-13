---
status: accepted
date: 2026-08-13
phase: "14a"
---

# 0482 — One consumer group per check; the central alert dispatcher is dead

**Context.** The alert checks all consume from `__system.job_requests`, which invites a single `alert.*` consumer that switches on job name to reach the right check. That dispatcher was built twice — first on 2026-08-02, again during the executor build — and killed both times.

**Decision.** Each check gets exactly one consumer group, bound to exactly its own job name. There is no central dispatcher; the binding table is the dispatch.

**Consequences.** Per-check operations fall out for free — a check can be suspended, retried, and observed independently, and adding a check never touches a shared switch. **Rejected:** a single `alert.*` consumer switching on job name — the switch re-invents the routing the binding table already owns.
