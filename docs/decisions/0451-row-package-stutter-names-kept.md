---
status: accepted
date: 2026-08-04
phase: "14a"
---

# 0451 — Consumption-loop package names keep the house stutter for now

**Context.** Names like `messageconsumer.MessageConsumerDefinition` stutter the package name into the type name, which Go style usually avoids — but the same stutter pattern exists across the codebase.

**Decision.** The new loop packages keep the house stutter. Any de-stuttering happens repo-wide at the v1 API review, not piecemeal per package.

**Consequences.** Naming stays consistent across all packages until one deliberate pass can fix the pattern everywhere at once. **Rejected:** fixing the stutter only in the new packages — it would leave two naming conventions live at the same time.
