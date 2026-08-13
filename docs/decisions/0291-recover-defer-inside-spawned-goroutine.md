---
status: accepted
date: 2026-07-12
phase: "9"
---

# 0291 — Panic recovery's defer lives inside the spawned goroutine and sends into done

**Context.** `recover()` only catches a panic on the same goroutine's call stack. Once the timeout race moved `consumerFunc` into its own goroutine, an earlier draft had that child goroutine assign a recovered panic straight into `callSafely`'s named return — an unsynchronized write racing the parent's own `return`, and functionally silent: on a panic, `done` never received anything, so `callSafely` waited out the entire timeout-plus-grace and returned a generic timeout error instead of the real panic.

**Decision.** The spawned goroutine's own `defer`/`recover()` converts the panic to an error and sends it into `done` like any other result — the parent learns of a panic through the same channel as a normal return.

**Consequences.** No data race (verified with `-race`); a panicking `consumerFunc` returns in about 120µs with the panic message and stack intact instead of after the full timeout. **Rejected:** writing the recovered value into the parent's named return from the child — a race and a silent misreport in one.
