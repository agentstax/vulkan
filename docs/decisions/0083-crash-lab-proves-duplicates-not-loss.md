---
status: accepted
date: 2026-06-20
phase: "3.5"
---

# 0083 — The crash lab proves off's cost is duplicate reruns, not loss, by SIGKILL with a widened WAL writer window

**Context.** The safety argument for `synchronous_commit=off` needed proof, not assertion. A crash test must make the lost-commit window large and deterministic rather than a race against the WAL writer's 200ms flush tick.

**Decision.** The lab (`examples/phase_1/crashlab`) logs every processed payload id to a file as the application's own durable record. Seed 5000 rows, `CHECKPOINT` so the backlog survives, `docker kill` Postgres at ~40% processed (SIGKILL, no graceful flush), restart through crash recovery, drain the reclaimed backlog. `wal_writer_delay` is widened to 10s so the lost set is large and deterministic — the same mechanism as production's ~200ms window, just bigger. Run identically under `off` and under `on` as control.

**Consequences.** Result: every seeded id processed at least once and all 5000 ended `done` under both settings — reprocessing, never loss. `off` lost 899 unflushed commits versus 85 under `on`; the 85 is irreducible in-flight work any at-least-once system reruns after a crash, so `off`'s entire cost is ~814 extra duplicates. `off` also kept 1301 commits since the checkpoint: the lost window is "since the last flush," not "since the last checkpoint." Both knobs were reset to defaults afterward and verified via `SHOW`.
