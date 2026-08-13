---
status: accepted
date: 2026-08-06
phase: "14a"
---

# 0410 — Topic display identity stays two structured fields, not a concatenated name@vN string

**Context.** With versioned topics, log lines and metrics need to identify which physical topic they describe. A single combined string like `orders@v2` is the common shorthand.

**Decision.** Every log line and metric carries `"topic"` (id or name) and `"version"` as separate structured fields; they are never concatenated.

**Consequences.** Filtering or aggregating by version alone works directly; a concatenated string would need parsing back apart anywhere that wants either half on its own.
