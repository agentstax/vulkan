---
status: accepted
date: 2026-07-20
phase: "11.5"
---

# 0355 — CLI flags map onto the sparse config structs via cmd.Flags().Changed

**Context.** `AlterConfig` distinguishes nil (leave alone) from an explicit zero value, but a flag's value alone cannot make that distinction — an unset bool flag and `--flag=false` read identically. Dated approximately; built across July 2026.

**Decision.** Flags map 1:1 onto the config structs, and only flags the operator actually passed — detected with `cmd.Flags().Changed` — become non-nil fields. Bools included: `--flag=false` is a real set, distinct from not passing the flag.

**Consequences.** The nil / explicit-zero tri-state survives end to end from the command line to the `COALESCE` UPDATE, so `vulkan topic alter` can set a field to its zero value or leave it alone, never confusing the two. `alter` renders an OLD → NEW diff table so the operator sees exactly which fields will change.
