---
status: accepted
date: 2026-08-04
phase: "14a"
---

# 0447 — uuid.UUID above the datastore, pgtype.UUID below; ErrLeaseLost declared where detected

**Context.** The pkg/consumer conversion carried a sweep of boundary cleanups: driver-specific id types and error sentinels had drifted across layers.

**Decision.** Ids cross the controller/datastore boundary as `uuid.UUID`; `pgtype.UUID` stays inside the datastore, the only layer that talks to the driver. `ErrLeaseLost` is declared in the package that detects the condition and re-bound in the controller so callers match it in domain vocabulary. Missing retry-Wraps were added (Bind/ClearBindings) and Wrap-only violations fixed (ReclaimWithCursor, FreshClaimMessagesWithCursor made private; dead limit params dropped).

**Consequences.** Driver shapes never leak above the SQL layer, and each sentinel error has one declaring home. About 140 lines of dead code went with the sweep.
