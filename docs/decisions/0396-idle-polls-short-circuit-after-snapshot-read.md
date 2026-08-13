---
status: accepted
date: 2026-08-01
phase: "14"
---

# 0396 — An idle poll short-circuits after the read-only snapshot statement

**Context.** The snapshot fence added a (head, xmax) read at the top of every cursor poll. Idle polling is the steady-state hot path for caught-up consumers, so the fence must not add per-tick write cost there.

**Decision.** When the read-only snapshot statement shows head == pending == settled == claimed, the poll returns immediately without opening the claim transaction.

**Consequences.** A truly idle poll is one read-only statement — this removed both the idle-tick cursor write and the idle `FOR UPDATE` that the claim-race fix had introduced. Measured idle at parity with the pre-fence shape (207µs vs 211µs at 10 partitions; 706µs vs 687µs at 500).
