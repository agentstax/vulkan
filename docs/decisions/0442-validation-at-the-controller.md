---
status: accepted
date: 2026-08-02
phase: "14a"
---

# 0442 — All input validation lives at the controller; datastores trust their inputs

**Context.** With the controller as the only door to persistence, input rules could live at the door, at the SQL layer, or both.

**Decision.** ALL input validation happens in `pkg/<x>/controller`. The datastore layer trusts what it is handed and does not re-check.

**Consequences.** Every call path crosses the door exactly once, so each rule is written and enforced once, and errors come back in domain vocabulary rather than SQL-layer terms. **Rejected:** re-checking in the datastore — a second copy of the same rules that drifts independently from the first.
