---
status: superseded
date: 2026-07-20
phase: "11.5"
---

# 0348 — AlterTopic takes a sparse pointer-per-field patch; nil means leave alone

**Context.** `RegisterTopic`'s `topic.Config` uses zero-means-default semantics — `WithDefaults` resolves unset fields before the row is written. That works for create but not for patch: a value field cannot distinguish "set RetentionTTL to 0" from "did not mention RetentionTTL," and zero is a real, often destructive setting here (RetentionTTL 0 = keep forever; a tuned JanitorSweepBatchSize silently reset). Dated approximately; built across July 2026.

**Decision.** `topic.AlterConfig` is pointer-per-field: nil = leave alone, non-nil = set, including an explicit zero. Its `Validate` is stricter than `Config.Validate` on the `> 0` fields, because there is no `WithDefaults` to absorb a zero — it would land in the row. An all-nil patch is a usage error, not a silent no-op.

**Consequences.** A forgotten field no-ops instead of clobbering a live value to zero: the same operator mistake fails safe under a sparse patch and fails destructively under full-replace. **Rejected:** reusing `topic.Config` / a full-replace struct — the operator must restate every field to change one, and any field they forget becomes zero and lands. **Rejected:** "require every field" — not enforceable in Go, since a missing struct field is a silent zero, not a compile error; the destructive version is what the language hands you by default.

Superseded by [0518]: topic config stops being operator-writable at all, so
AlterTopic and its patch shape are deleted rather than reshaped.
