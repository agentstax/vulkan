---
status: accepted
date: 2026-09-01
phase: "pre-v1"
---

# 0628 — The table-name functions are public API

**Context.** [0371] moved `MessageLogTable` and its siblings into `internal/topic` on the premise that they were cross-package plumbing: "zero example programs call them, and every real call site is `pkg/producer`/`pkg/consumer` building raw SQL against tables `pkg/topic` owns." That premise no longer holds. Labs cannot import `internal/`, so every lab that touches a per-topic table interpolates `message_log_%d` by hand, and CONVENTIONS ## Tables had to carry a standing exception saying so — a second way of spelling a name the library already computes one way, which is exactly the duplicate-mechanism smell the file bans. The schema work [0629] adds a second caller the internal package cannot serve: an operator pasting a diagnose query needs the table's real name, and per-topic names are the half no catalog lookup gives them.

**Decision.** `internal/topic/tables.go` becomes `pkg/topic/tables.go`; the twelve funcs are public API on the vocabulary root. Every call site drops the `iTopic` alias and spells the package `topic` — the alias existed only because [0371]'s `pkg/topic/datastore.go` held local `topic *Topic` variables that would shadow it, and that file is gone. The repo-root `internal/` directory held nothing else and is deleted. CONVENTIONS ## Tables loses the lab exception: everything names a per-topic table through these funcs.

**Consequences.** Supersedes [0371], which is marked superseded. Twelve function names join the public surface permanently and cannot be renamed without a breaking change — accepted, because a per-topic table's name is already public in every sense that matters: it is in the DDL, in error text, and in the diagnose queries. `tools/conventions`'s DDL walk reads the funcs from their new path. **Rejected:** keeping the funcs internal and giving labs a duplicate helper — two derivations of one name is the thing [0371]'s exception already was.
