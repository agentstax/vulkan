---
status: accepted
date: 2026-08-04
phase: "14a"
---

# 0450 — rangeState and claimBuffer stay private, not their own packages

**Context.** The package split raised the question of whether rangeState and claimBuffer, each used by several collaborators, should become packages of their own.

**Decision.** They stay private inside their owning package. Three things reach into their internals, and a package boundary there would have to export the very atomics whose correctness rests on unexported write ordering.

**Consequences.** The write-ordering invariant stays enforceable by the compiler — nothing outside the package can touch the atomics out of order. The collaborators live together rather than behind an interface. **Rejected:** promoting them to packages — the exported surface required would be exactly the unsafe one.
