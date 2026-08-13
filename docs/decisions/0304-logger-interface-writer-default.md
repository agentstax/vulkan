---
status: accepted
date: 2026-07-14
phase: "10"
---

# 0304 — Logging goes through a caller-supplied interface with an io.Writer-backed default

**Context.** Before this, the library had no operator-facing log surface; adding one meant choosing between hardcoding a logging implementation and letting callers supply their own.

**Decision.** `pkg` logging accepts a logger interface, not a hardcoded implementation. A default `io.Writer`-backed implementation ships for callers with no opinion.

**Consequences.** Callers plug in their own logging stack without the library importing any logging framework; callers who do not care get working output with zero setup.
