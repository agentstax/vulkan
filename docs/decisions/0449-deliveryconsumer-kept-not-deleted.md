---
status: accepted
date: 2026-08-04
phase: "14a"
---

# 0449 — deliveryconsumer kept in the tree but not wired to run

**Context.** The delivery_consumer loop is a strictly more expensive CURSOR over the same messages, so nothing in the current feature set justifies running it — but the non-FIFO queue work on the roadmap might.

**Decision.** deliveryconsumer stays in the tree as its own package under pkg/consumer, not deleted, and does not run. It re-earns its place only if non-FIFO queue work needs it.

**Consequences.** The per-loop package layout makes the eventual outcome cheap either way: reviving it is wiring an existing package, and deleting it is a whole-directory drop. **Rejected:** deleting now — the roadmap gives it a plausible future, and the layout makes carrying it nearly free.
