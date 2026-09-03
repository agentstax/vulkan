---
status: accepted
date: 2026-09-02
phase: "pre-v1"
---

# 0641 — Run is a per-caller loop; the row is the only arbiter

**Context.** [0638] made `SystemManager.Run` join-and-block: a mutex-guarded
count of active callers, the first join starting a shared reconcile loop under
an internal context, the last one out draining it. Review of the built code
found the count has no users. The scheduler builds a new `SystemManager` per
`Schedule` call, so that instance can only ever have one caller;
`client.RunManager` is called exactly once in every example and in the CLI.
And the case the count anticipated — several `Consume`s sharing the client's
manager — is already what `target_instances = 1` arbitrates: N loops attempt
the claim, one holds it, the rest are a declined insert per RetryDelay,
identical to N processes, which the design embraces. An in-process caller
count was a second arbitration mechanism beside the row's — the
one-mechanism-per-fact violation.

**Decision.** `Run` resolves the system owner, builds a `manager.Runner`, and
runs it under the caller's own ctx inside [0640]'s re-claim/backoff loop
(VK0065 with error, attempt, delay; `Config.RunRetry` between lives). The
shared state is deleted wholesale: mutex, `callers`, `stop`, `done`, the
`WithoutCancel` loop context, and the join/leave/reconcile methods.
`SystemManager` holds only its dependencies; N `Run` calls on one instance
are N runners.

**Consequences.** The one behavior lost is gap-free in-process handover: when
the claiming caller cancels, its claim is released and the next runner —
in-process or another process — claims within `RetryDelay`, the takeover
latency the cross-process story already accepts. Everything else holds and
was re-verified live under `-race`: a second `Run` on one client is admitted,
one instance row is live with two callers, the remaining caller takes over
within the retry delay, the last one out releases the claim, and a later
`Run` claims fresh. Supersedes [0638]'s join-and-block clause; the rest of
0638 stands — the permit stays deleted, nothing is refused, no caller
receives a fatal. [0640]'s re-claim decision stands, now inside `Run`; its
`stop != nil` bookkeeping observation is moot since there is no `stop`.
**Rejected:** keeping the count for the gap-free handover — machinery in
every deployment paying for a ≤RetryDelay upkeep pause in the rare one.
